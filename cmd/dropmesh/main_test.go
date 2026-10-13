package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dropmesh/internal/auth"
	"dropmesh/internal/protocol"
	"dropmesh/internal/resume"
)

func TestSenderAndReceiverBinaries(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "dropmesh")
	if os.PathSeparator == '\\' {
		binary += ".exe"
	}
	goExe, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("locate Go executable: %v", err)
	}
	if goRoot := os.Getenv("GOROOT"); goRoot != "" {
		goName := "go"
		if os.PathSeparator == '\\' {
			goName += ".exe"
		}
		goExe = filepath.Join(goRoot, "bin", goName)
	}
	build := exec.Command(goExe, "build", "-o", binary, "./cmd/dropmesh")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	work := t.TempDir()
	source := filepath.Join(work, "payload.bin")
	destination := filepath.Join(work, "received")
	if err = os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte{0, 4, 255, 8}, 600000)
	if err = os.WriteFile(source, payload, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	sender := exec.CommandContext(ctx, binary, "send", source, "--listen", "127.0.0.1:0", "--advertise", "127.0.0.1")
	sender.Dir = root
	stdout, err := sender.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var senderErr bytes.Buffer
	sender.Stderr = &senderErr
	if err = sender.Start(); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024), auth.MaxInvite+1024)
	invite := ""
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), auth.InvitePrefix) {
			invite = scanner.Text()
			break
		}
	}
	if invite == "" {
		_ = sender.Process.Kill()
		t.Fatalf("sender did not offer an invite: %v", scanner.Err())
	}
	receiver := exec.CommandContext(ctx, binary, "receive", "--invite-stdin", "--yes", "--out", destination)
	receiver.Dir = root
	receiver.Stdin = strings.NewReader(invite + "\n")
	output, receiveErr := receiver.CombinedOutput()
	if receiveErr != nil {
		_ = sender.Process.Kill()
		t.Fatalf("receiver failed: %v\n%s", receiveErr, output)
	}
	if err = sender.Wait(); err != nil {
		t.Fatalf("sender failed: %v\n%s", err, senderErr.String())
	}
	if !strings.Contains(senderErr.String(), "File: payload.bin") || !strings.Contains(senderErr.String(), "Route: DIRECT") {
		t.Fatalf("sender status missing file or route facts: %s", senderErr.String())
	}
	if !strings.Contains(string(output), "Incoming file: payload.bin") || !strings.Contains(string(output), "Encryption: TLS 1.3") {
		t.Fatalf("receiver did not display the offer and measured route facts: %s", output)
	}
	got, err := os.ReadFile(filepath.Join(destination, "payload.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("binary transfer content mismatch")
	}
}

func TestResumableCLIProcessesRestartAfterReceiverKill(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "dropmesh")
	if os.PathSeparator == '\\' {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "./cmd/dropmesh")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	work := t.TempDir()
	source := filepath.Join(work, "restart.bin")
	outDir := filepath.Join(work, "received")
	if err = os.Mkdir(outDir, 0700); err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("cli-restart-data-"), 1<<20)
	if err = os.WriteFile(source, payload, 0600); err != nil {
		t.Fatal(err)
	}
	transferID := "1234567890abcdef1234567890abcdef"
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	firstSender, firstInvite, firstErrOut := startCLISender(t, ctx, root, binary, source, transferID)
	firstReceiver := exec.CommandContext(ctx, binary, "receive", "--invite-stdin", "--yes", "--out", outDir)
	firstReceiver.Dir = root
	firstReceiver.Stdin = strings.NewReader(firstInvite + "\n")
	receiverErr, err := firstReceiver.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = firstReceiver.Start(); err != nil {
		t.Fatal(err)
	}
	progress := make(chan []byte, 32)
	go func() {
		buf := make([]byte, 1024)
		for {
			n, readErr := receiverErr.Read(buf)
			if n > 0 {
				progress <- append([]byte(nil), buf[:n]...)
			}
			if readErr != nil {
				return
			}
		}
	}()
	var observed bytes.Buffer
	for !bytes.Contains(observed.Bytes(), []byte("Receiving 1048576/")) {
		select {
		case chunk := <-progress:
			_, _ = observed.Write(chunk)
		case <-ctx.Done():
			_ = firstReceiver.Process.Kill()
			t.Fatalf("receiver never reported its first committed chunk: %s", observed.String())
		}
	}
	if err = firstReceiver.Process.Kill(); err != nil {
		t.Fatalf("kill interrupted receiver: %v", err)
	}
	_ = firstReceiver.Wait()
	if err = firstSender.Process.Kill(); err != nil {
		t.Fatalf("kill interrupted sender: %v", err)
	}
	_ = firstSender.Wait()
	if !strings.Contains(firstErrOut.String(), "Transfer ID: "+transferID) {
		t.Fatalf("sender did not report stable transfer ID: %s", firstErrOut.String())
	}

	descriptor := protocol.Descriptor{TransferID: transferID, Name: filepath.Base(source), Size: uint64(len(payload)), ChunkSize: protocol.ChunkSize, SHA256: sha256.Sum256(payload)}
	store, err := resume.Open(outDir, descriptor, uint64(len(payload)))
	if err != nil {
		t.Fatal(err)
	}
	committed := store.CommittedChunks()
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if committed == 0 || committed >= (uint64(len(payload))+protocol.ChunkSize-1)/protocol.ChunkSize {
		t.Fatalf("killed receiver preserved %d chunks; expected a strict partial prefix", committed)
	}

	secondSender, secondInvite, secondErrOut := startCLISender(t, ctx, root, binary, source, transferID)
	secondReceiver := exec.CommandContext(ctx, binary, "receive", "--invite-stdin", "--yes", "--out", outDir)
	secondReceiver.Dir = root
	secondReceiver.Stdin = strings.NewReader(secondInvite + "\n")
	if output, err := secondReceiver.CombinedOutput(); err != nil {
		_ = secondSender.Process.Kill()
		t.Fatalf("resumed receiver failed: %v\n%s", err, output)
	}
	if err = secondSender.Wait(); err != nil {
		t.Fatalf("resumed sender failed: %v\n%s", err, secondErrOut.String())
	}
	got, err := os.ReadFile(filepath.Join(outDir, filepath.Base(source)))
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("resumed CLI output mismatch: len=%d err=%v", len(got), err)
	}
}

func startCLISender(t *testing.T, ctx context.Context, root, binary, source, transferID string) (*exec.Cmd, string, *bytes.Buffer) {
	t.Helper()
	cmd := exec.CommandContext(ctx, binary, "send", source, "--resume", transferID, "--listen", "127.0.0.1:0", "--advertise", "127.0.0.1")
	cmd.Dir = root
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024), auth.MaxInvite+1024)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), auth.InvitePrefix) {
			return cmd, scanner.Text(), &stderr
		}
	}
	_ = cmd.Process.Kill()
	t.Fatalf("CLI sender did not print invite: %v (%s)", scanner.Err(), stderr.String())
	return nil, "", nil
}

func TestArgumentReordering(t *testing.T) {
	got, err := reorder([]string{"file.bin", "--listen", "127.0.0.1:0", "--advertise=127.0.0.1"}, map[string]bool{"listen": true, "advertise": true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--listen", "127.0.0.1:0", "--advertise=127.0.0.1", "file.bin"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestVersionAndExitCodes(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"version"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.HasPrefix(out.String(), "dropmesh ") {
		t.Fatalf("version result: code=%d output=%q", code, out.String())
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"send"}, strings.NewReader(""), &out, &errOut); code != 2 {
		t.Fatalf("invalid usage exit code=%d, want 2", code)
	}
	errOut.Reset()
	if code := run([]string{"receive", "--invite-stdin"}, strings.NewReader(""), &out, &errOut); code != 2 {
		t.Fatalf("unsafe unattended input exit code=%d, want 2", code)
	}
	errOut.Reset()
	if code := run([]string{"resume", "delete", "bad-id"}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Fatalf("invalid resume ID exit code=%d, want 1", code)
	}
	errOut.Reset()
	missingStateDir := t.TempDir()
	if code := run([]string{"resume", "delete", "0123456789abcdef0123456789abcdef", "--out", missingStateDir}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("delete absent state exit code=%d, output=%q", code, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(missingStateDir, ".dropmesh-state")); !os.IsNotExist(err) {
		t.Fatalf("deleting absent state created a state directory: %v", err)
	}
	errOut.Reset()
	if code := run([]string{"send", filepath.Join(t.TempDir(), "missing")}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Fatalf("operational failure exit code=%d, want 1", code)
	}
}

func TestSanitizeRedactsAuthenticationValues(t *testing.T) {
	for _, input := range []string{"secret=do-not-print", "capability abc123", "certificate=private-material"} {
		got := sanitize(input)
		if strings.Contains(got, "do-not-print") || strings.Contains(got, "abc123") || strings.Contains(got, "private-material") {
			t.Fatalf("sensitive value survived redaction: %q", got)
		}
	}
}

func Example_percentage() {
	fmt.Printf("%.0f%%\n", percentage(1, 4)) // Output: 25%
}
