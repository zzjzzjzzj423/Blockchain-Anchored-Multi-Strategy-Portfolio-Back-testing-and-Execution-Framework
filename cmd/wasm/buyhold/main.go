package main

import "msbd5017/portfolio-proof/strategies/buyhold"

// This portable module pins the deterministic core used by the Argo adapter.
// Host registration remains outside the core so Argo's fill semantics cannot
// change the frozen execution-accounting evidence.
func main() { _ = buyhold.New() }
