package proc

import (
	"context"
	"syscall"
	"testing"
	"time"
)

func TestSpawnStdinWait(t *testing.T) {
	ctx := context.Background()
	resp := Spawn(ctx, SpawnRequest{Cmd: "bash", Args: []string{"-c", "read line; echo hi $line"}})
	if resp.Error != "" {
		t.Fatalf("spawn error: %v", resp.Error)
	}
	pid := resp.Pid
	defer Kill(ctx, KillRequest{Pid: pid, Signal: int(syscall.SIGKILL)})

	sresp := Stdin(ctx, StdinRequest{Pid: pid, Data: "world\n"})
	if sresp.Error != "" {
		t.Fatalf("stdin error: %v", sresp.Error)
	}
	wresp := Wait(ctx, WaitRequest{Pid: pid, TimeoutMs: 5000})
	if wresp.Error != "" {
		t.Fatalf("wait error: %v", wresp.Error)
	}
	if wresp.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", wresp.ExitCode)
	}
	if wresp.Stdout != "hi world\n" {
		t.Fatalf("unexpected stdout: %q", wresp.Stdout)
	}
}

func TestKill(t *testing.T) {
	ctx := context.Background()
	resp := Spawn(ctx, SpawnRequest{Cmd: "sleep", Args: []string{"1000"}})
	if resp.Error != "" {
		t.Fatalf("spawn error: %v", resp.Error)
	}
	pid := resp.Pid
	kresp := Kill(ctx, KillRequest{Pid: pid})
	if kresp.Error != "" || !kresp.Killed {
		t.Fatalf("kill failed: %+v", kresp)
	}
	wresp := Wait(ctx, WaitRequest{Pid: pid, TimeoutMs: 5000})
	if wresp.Error != "" {
		t.Fatalf("wait error: %v", wresp.Error)
	}
	if wresp.ExitCode == 0 {
		t.Fatalf("expected non-zero exit after kill")
	}
}

func TestList(t *testing.T) {
	ctx := context.Background()
	resp := Spawn(ctx, SpawnRequest{Cmd: "sleep", Args: []string{"1"}})
	if resp.Error != "" {
		t.Fatalf("spawn error: %v", resp.Error)
	}
	pid := resp.Pid
	found := false
	l := List(ctx, ListRequest{})
	for _, p := range l.Processes {
		if p.Pid == pid {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("pid %d not in list", pid)
	}
	_ = Wait(ctx, WaitRequest{Pid: pid, TimeoutMs: int(2 * time.Second.Milliseconds())})
}
