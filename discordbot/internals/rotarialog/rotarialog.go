package rotarialog

import (
	"fmt"
	"log"
	"os"
	"runtime/debug"
)

type RotariaLog struct {
	WarningLogger *log.Logger
	InfoLogger    *log.Logger
	ErrorLogger   *log.Logger
}

const (
	DEBUG   = iota
	WARNING = iota
	INFO    = iota
	ERROR   = iota
)

var (
	files = [...]string{"INFO.log", "WARNING.log", "ERROR.log", "DEBUG.log"}
)

func (RotariaLog *RotariaLog) InitLogDir() {
	// files := [...]string{"INFO", "WARNING", "ERROR", "DEBUG"}
	for _, f := range files {
		_, err := os.OpenFile(fmt.Sprintf("logs/%s", f), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
		if err != nil {
			fmt.Println("Error creating file")
			return
		}
	}
}

func (RotariaLog *RotariaLog) WriteLog(logLevel uint64, msg string, err error, isFatal bool) {
	switch logLevel {
	case DEBUG:
		f, _ := os.OpenFile("logs/DEBUG.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
		log.SetOutput(f)
		return
	case INFO:
		if isFatal {
			fmt.Println("Log of type 'info' can not be fatal")
			return
		}
		f, _ := os.OpenFile("logs/INFO.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
		RotariaLog.InfoLogger = log.New(f, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile)
		RotariaLog.InfoLogger.Printf("%s: %s\n", msg, err)
	case WARNING:
		f, _ := os.OpenFile("logs/WARNING.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
		RotariaLog.WarningLogger = log.New(f, "WARNING: ", log.Ldate|log.Ltime|log.Lshortfile)
		if isFatal {
			RotariaLog.WarningLogger.Output(2, fmt.Sprintf("%s: %s (FATAL)", msg, err))
			f.Write(debug.Stack())
			os.Exit(1)
		} else {
			RotariaLog.WarningLogger.Output(2, fmt.Sprintf("%s: %s", msg, err))
		}
	case ERROR:
		f, _ := os.OpenFile("logs/ERROR.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
		RotariaLog.ErrorLogger = log.New(f, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
		if isFatal {
			f.Write(debug.Stack())
			RotariaLog.ErrorLogger.Fatalf("%s: %s (FATAL)", msg, err)
		} else {
			RotariaLog.ErrorLogger.Printf("%s: %s", msg, err)
		}
	}
}

func (RotariaLog *RotariaLog) CleanLogs() {
	for _, f := range files {
		if err := os.Remove(fmt.Sprintf("logs/%s", f)); err != nil {
			log.Panic("Could not remove files")
		}
	}
}
