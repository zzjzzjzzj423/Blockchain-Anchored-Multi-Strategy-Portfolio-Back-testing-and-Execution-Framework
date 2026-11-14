package main

import "msbd5017/portfolio-proof/strategies/meanreversion"

func main() { _ = meanreversion.MustNew(20, -1, 0) }
