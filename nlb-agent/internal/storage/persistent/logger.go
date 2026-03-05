package persistent

import (
	"fmt"

	"github.com/rs/zerolog"
)

type loggerWrapper struct {
	log *zerolog.Logger
}

func (l loggerWrapper) Debug(v ...interface{}) {
	l.log.Debug().Msg(fmt.Sprint(v...))
}
func (l loggerWrapper) Debugf(format string, v ...interface{}) {
	l.log.Debug().Msgf(format, v...)
}

func (l loggerWrapper) Error(v ...interface{}) {
	l.log.Error().Msg(fmt.Sprint(v...))
}
func (l loggerWrapper) Errorf(format string, v ...interface{}) {
	l.log.Error().Msgf(format, v...)
}

func (l loggerWrapper) Info(v ...interface{}) {
	l.log.Info().Msg(fmt.Sprint(v...))
}
func (l loggerWrapper) Infof(format string, v ...interface{}) {
	l.log.Info().Msgf(format, v...)
}

func (l loggerWrapper) Warning(v ...interface{}) {
	l.log.Warn().Msg(fmt.Sprint(v...))
}
func (l loggerWrapper) Warningf(format string, v ...interface{}) {
	l.log.Warn().Msgf(format, v...)
}

func (l loggerWrapper) Fatal(v ...interface{}) {
	l.log.Fatal().Msg(fmt.Sprint(v...))
}
func (l loggerWrapper) Fatalf(format string, v ...interface{}) {
	l.log.Fatal().Msgf(format, v...)
}

func (l loggerWrapper) Panic(v ...interface{}) {
	l.log.Panic().Msg(fmt.Sprint(v...))
}
func (l loggerWrapper) Panicf(format string, v ...interface{}) {
	l.log.Panic().Msgf(format, v...)
}
