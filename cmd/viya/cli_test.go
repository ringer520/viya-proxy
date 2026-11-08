package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseCLIArgs(t *testing.T) {
	cases := []struct {
		args   []string
		want   cliAction
		wantErr bool
	}{
		{nil, cliStart, false},
		{[]string{}, cliStart, false},
		{[]string{"version"}, cliVersion, false},
		{[]string{"--version"}, cliVersion, false},
		{[]string{"-v"}, cliVersion, false},
		{[]string{"help"}, cliHelp, false},
		{[]string{"--help"}, cliHelp, false},
		{[]string{"-h"}, cliHelp, false},
		{[]string{"version", "--json"}, cliStart, true},
		{[]string{"serve"}, cliStart, true},
		{[]string{"--config", "x"}, cliStart, true},
	}
	for _, tc := range cases {
		got, err := parseCLIArgs(tc.args)
		if (err != nil) != tc.wantErr {
			t.Errorf("args=%v err=%v wantErr=%v", tc.args, err, tc.wantErr)
			continue
		}
		if !tc.wantErr && got != tc.want {
			t.Errorf("args=%v action=%d want=%d", tc.args, got, tc.want)
		}
	}
}

func TestPrintVersionIncludesIdentityAndSchema(t *testing.T) {
	var out bytes.Buffer
	printVersion(&out)
	text := out.String()
	for _, want := range []string{"Viya", version, buildCommit, "schema version", "runtime"} {
		if !strings.Contains(text, want) {
			t.Errorf("version output missing %q: %s", want, text)
		}
	}
}

func TestInstanceLockRejectsSecondProcessOnSameDataDir(t *testing.T) {
	dir := t.TempDir()
	release, err := acquireInstanceLock(dir)
	if err != nil {
		t.Fatalf("first lock failed: %v", err)
	}
	if _, err := acquireInstanceLock(dir); err == nil {
		t.Fatal("second lock on the same data directory must be refused")
	}
	release()
	if release2, err := acquireInstanceLock(dir); err != nil {
		t.Fatalf("lock not reusable after release: %v", err)
	} else {
		release2()
	}
}

func TestInstanceLockAllowsSeparateDataDirs(t *testing.T) {
	// A staged install or a test may intentionally use another directory.
	releaseA, err := acquireInstanceLock(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseA()
	releaseB, err := acquireInstanceLock(t.TempDir())
	if err != nil {
		t.Fatalf("independent data directory was blocked: %v", err)
	}
	releaseB()
}
