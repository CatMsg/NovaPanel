package service

var subServerRestartFunc func() error

func SetSubServerRestartFunc(fn func() error) {
	subServerRestartFunc = fn
}

func restartSubServer() error {
	if subServerRestartFunc == nil {
		return nil
	}
	return subServerRestartFunc()
}
