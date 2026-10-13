package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"dropmesh/internal/auth"
	"dropmesh/internal/resume"
	"dropmesh/internal/transfer"
)

var version = "dev"

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		usage(out)
		return 0
	}
	if args[0] == "version" {
		_, _ = fmt.Fprintf(out, "dropmesh %s\n", version)
		return 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	switch args[0] {
	case "send":
		err = runSend(ctx, args[1:], out, errOut)
	case "receive":
		err = runReceive(ctx, args[1:], in, errOut)
	case "resume":
		err = runResume(args[1:], errOut)
	default:
		usage(errOut)
		return 2
	}
	if err == nil {
		return 0
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		_, _ = fmt.Fprintf(errOut, "Canceled.\n")
		return 130
	}
	_, _ = fmt.Fprintf(errOut, "dropmesh: %s\n", sanitize(err.Error()))
	if errors.Is(err, errUsage) {
		return 2
	}
	return 1
}

var errUsage = errors.New("invalid usage")

func runSend(ctx context.Context, args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	listen := fs.String("listen", "0.0.0.0:0", "UDP listen address")
	advertise := fs.String("advertise", "", "IPv4 address receivers can reach")
	resumeID := fs.String("resume", "", "resume the transfer with this transfer ID")
	reordered, err := reorder(args, map[string]bool{"listen": true, "advertise": true, "resume": true})
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if err = fs.Parse(reordered); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("%w: usage: dropmesh send FILE [--resume TRANSFER_ID] [--listen ADDRESS] [--advertise IP]", errUsage)
	}
	err = transfer.Send(ctx, transfer.SenderOptions{File: fs.Arg(0), Listen: *listen, Advertise: *advertise, TransferID: *resumeID, Status: errOut, Progress: func(done, total uint64) {
		_, _ = fmt.Fprintf(errOut, "\rSending %d/%d bytes (%.1f%%)", done, total, percentage(done, total))
	}}, out)
	if errors.Is(err, context.Canceled) {
		return err
	}
	return err
}

func runResume(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "delete" {
		return fmt.Errorf("%w: usage: dropmesh resume delete TRANSFER_ID [--out DIRECTORY]", errUsage)
	}
	fs := flag.NewFlagSet("resume delete", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	outDir := fs.String("out", ".", "output directory containing receiver state")
	reordered, err := reorder(args[1:], map[string]bool{"out": true})
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if err = fs.Parse(reordered); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("%w: usage: dropmesh resume delete TRANSFER_ID [--out DIRECTORY]", errUsage)
	}
	if err = resume.Delete(*outDir, fs.Arg(0)); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Deleted resumable state %s.\n", fs.Arg(0))
	return err
}

func runReceive(ctx context.Context, args []string, in io.Reader, errOut io.Writer) error {
	fs := flag.NewFlagSet("receive", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	outDir := fs.String("out", ".", "output directory")
	yes := fs.Bool("yes", false, "accept transfer without interactive confirmation")
	stdin := fs.Bool("invite-stdin", false, "read invite from stdin")
	max := fs.Uint64("max-bytes", transfer.DefaultMaxBytes, "maximum accepted file size in bytes")
	reordered, err := reorder(args, map[string]bool{"out": true, "max-bytes": true})
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if err = fs.Parse(reordered); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("%w: usage: dropmesh receive [--out DIRECTORY] [--yes] [--max-bytes BYTES]", errUsage)
	}
	var invite string
	var inviteReader io.Reader
	if *stdin {
		if !*yes {
			return fmt.Errorf("%w: --invite-stdin requires --yes", errUsage)
		}
		inviteReader = io.LimitReader(in, auth.MaxInvite+1)
	} else {
		_, _ = fmt.Fprint(errOut, "Paste invite: ")
		line, e := bufio.NewReader(io.LimitReader(in, auth.MaxInvite+1)).ReadString('\n')
		if e != nil && e != io.EOF {
			return fmt.Errorf("read invite: %w", e)
		}
		invite = strings.TrimSpace(line)
	}
	started := time.Now()
	var received uint64
	path, err := transfer.Receive(ctx, transfer.ReceiverOptions{Invite: invite, InviteReader: inviteReader, OutputDir: *outDir, Yes: *yes, MaxBytes: *max, Status: errOut, Progress: func(done, total uint64) {
		received = done
		_, _ = fmt.Fprintf(errOut, "\rReceiving %d/%d new bytes (%.1f%%)", done, total, percentage(done, total))
	}})
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect published file: %w", err)
	}
	elapsed := time.Since(started)
	seconds := elapsed.Seconds()
	if seconds <= 0 {
		seconds = 0.000001
	}
	_, err = fmt.Fprintf(errOut, "\nSaved: %s\nRoute: DIRECT\nTransport: QUIC\nEncryption: TLS 1.3\nFile size: %d bytes\nReceived this run: %d bytes\nElapsed: %s\nThroughput this run: %.2f MiB/s\n", path, info.Size(), received, elapsed.Round(time.Millisecond), float64(received)/seconds/1048576)
	return err
}

func percentage(done, total uint64) float64 {
	if total == 0 {
		return 100
	}
	return float64(done) * 100 / float64(total)
}

func reorder(args []string, values map[string]bool) ([]string, error) {
	var options, positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "--") {
			positionals = append(positionals, arg)
			continue
		}
		name, value, has := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		if value != "" || has {
			options = append(options, arg)
			continue
		}
		if !values[name] {
			options = append(options, arg)
			continue
		}
		if i+1 >= len(args) {
			return nil, fmt.Errorf("--%s requires a value", name)
		}
		i++
		options = append(options, arg, args[i])
	}
	return append(options, positionals...), nil
}

func sanitize(s string) string {
	for _, secret := range []string{"secret=", "certificate=", "private key", "capability"} {
		if strings.Contains(strings.ToLower(s), secret) {
			return "operation failed (sensitive authentication data redacted)"
		}
	}
	return s
}

func usage(w io.Writer) {
	_, _ = io.WriteString(w, "DropMesh: direct, authenticated file transfer\n\nUsage:\n  dropmesh send FILE [--resume TRANSFER_ID] [--listen ADDRESS] [--advertise IP]\n  dropmesh receive [--out DIRECTORY] [--yes] [--max-bytes BYTES]\n  dropmesh receive --invite-stdin --yes [receiver options]\n  dropmesh resume delete TRANSFER_ID [--out DIRECTORY]\n  dropmesh version\n\nA new one-use invite is generated for every send, including resumed sends. The receiver stores incomplete state under the selected output directory.\nReceiver invite input is prompted interactively unless --invite-stdin is used.\n")
}
