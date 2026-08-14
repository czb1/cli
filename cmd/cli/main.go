package main

import "omres-cli/internal/cli"

// 由流水线 ldflags 注入：
//
//	-X 'main.BuildTime=...' -X 'main.CommitID=...' -X 'main.BuildFlavor=...'
//
// 必须是 package 级 string 变量、且不能是 const，否则 -X 静默失效。
var (
	BuildTime   = ""
	CommitID    = ""
	BuildFlavor = "" // product | gray | hlt；本地 go build 时为空 → 不自动升级
)

func main() {
	cli.SetBuildInfo(BuildTime, CommitID, BuildFlavor)
	cli.Run()
}
