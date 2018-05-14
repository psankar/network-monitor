package minion

type MinionResponse struct {
	Result bool
}

type DoesExistReq struct {
	Path string
}

type DoesContainReq struct {
	Path  string
	Check string
}

type IsRunningReq struct {
	ProcessName string
}
