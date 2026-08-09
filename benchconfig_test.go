package antedom

// Benchmark runs are recorded historically and compared across revisions
// (benchstat), so benchmark output must be self-describing. When -bench is
// set, TestMain prints benchfmt configuration lines ("key: value") ahead of
// the results; benchstat groups and filters by them. The keys are an
// append-only schema: add new ones freely, never rename or repurpose one.
//
// This file references no other symbol in the package so that it compiles
// unchanged at any revision.

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	flag.Parse()
	if f := flag.Lookup("test.bench"); f != nil && f.Value.String() != "" {
		printBenchConfig()
	}
	os.Exit(m.Run())
}

// printBenchConfig emits one configuration line per fact. A helper that
// fails (no git, no uname) skips its line rather than failing the run: a
// benchmark at an old revision must never break on missing tooling. The
// testing package itself adds goos, goarch, pkg, and (where detectable)
// cpu; GOMAXPROCS is the -N suffix on each benchmark name.
func printBenchConfig() {
	if label := os.Getenv("BENCH_LABEL"); label != "" {
		fmt.Printf("host: %s\n", label)
	} else if host, err := os.Hostname(); err == nil {
		fmt.Printf("host: %s\n", host)
	}
	if out, err := exec.Command("uname", "-sr").Output(); err == nil {
		fmt.Printf("kernel: %s\n", strings.TrimSpace(string(out)))
	}
	// AC versus battery shifts sustained clocks on laptops. pmset is
	// darwin-only; elsewhere the line is skipped.
	if out, err := exec.Command("pmset", "-g", "batt").Output(); err == nil {
		switch {
		case strings.Contains(string(out), "AC Power"):
			fmt.Printf("power: AC\n")
		case strings.Contains(string(out), "Battery Power"):
			fmt.Printf("power: battery\n")
		}
	}
	// Test binaries embed neither VCS info (-buildvcs skips them) nor a
	// module dependency list (ReadBuildInfo returns empty Deps), so ask
	// git and the go tool; the working directory of a test binary is the
	// package directory. Untracked files do not trip --dirty, only
	// modified tracked ones.
	if out, err := exec.Command("git", "describe", "--always", "--dirty").Output(); err == nil {
		fmt.Printf("commit: %s\n", strings.TrimSpace(string(out)))
	}
	// The committer date, distinct from the run date below: historical
	// sweeps run old commits long after they were written, and plots
	// over history want the commit's own timeline.
	if out, err := exec.Command("git", "show", "-s", "--format=%cI").Output(); err == nil {
		fmt.Printf("commit-date: %s\n", strings.TrimSpace(string(out)))
	}
	fmt.Printf("date: %s\n", time.Now().Format(time.RFC3339))
	fmt.Printf("go-version: %s\n", runtime.Version())
	fmt.Printf("gomaxprocs: %d\n", runtime.GOMAXPROCS(0))
	fmt.Printf("numcpu: %d\n", runtime.NumCPU())
	for _, env := range []string{"GOGC", "GOFLAGS", "GOEXPERIMENT"} {
		if v := os.Getenv(env); v != "" {
			fmt.Printf("%s: %s\n", strings.ToLower(env), v)
		}
	}
	for _, name := range []string{"test.benchtime", "test.count"} {
		if f := flag.Lookup(name); f != nil {
			fmt.Printf("%s: %s\n", strings.TrimPrefix(name, "test."), f.Value.String())
		}
	}
	if out, err := exec.Command("go", "list", "-m", "all").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			switch fields[0] {
			case "github.com/grafana/sobek":
				fmt.Printf("dep-sobek: %s\n", fields[1])
			case "github.com/yuin/goldmark":
				fmt.Printf("dep-goldmark: %s\n", fields[1])
			case "golang.org/x/net":
				fmt.Printf("dep-x-net: %s\n", fields[1])
			}
		}
	}
}
