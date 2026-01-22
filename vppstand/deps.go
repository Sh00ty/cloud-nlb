package vppstand

type Logger interface {
	Errorf(format string, args ...any)
	Infof(format string, args ...any)
}
