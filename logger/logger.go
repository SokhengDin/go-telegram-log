package logger

import (
	"io"
	"log"
	"os"

	"gopkg.in/lumberjack.v2"
)

// Init configures the default log package to write to both stdout and
// logs/app.log with daily rotation and 30-day retention.
func Init(path string) {
	rotator := &lumberjack.Logger{
		Filename:   path,
		MaxSize:    50,   // megabytes before rotation
		MaxBackups: 10,   // number of rotated files to keep
		MaxAge:     30,   // days
		Compress:   true, // gzip old logs
		LocalTime:  true,
	}

	multi := io.MultiWriter(os.Stdout, rotator)
	log.SetOutput(multi)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile)
}
