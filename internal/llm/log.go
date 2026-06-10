package llm

import (
	"log"
	"os"
	"path/filepath"
)

func init() {
	// 所有包共用一个日志文件，只需要初始化一次
	// 其他包（agent、context、tui）的 init() 不再需要重复设置
	logFile, err := os.OpenFile(filepath.Join(os.TempDir(), "codebuddy.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(logFile)
	}
	log.SetFlags(log.Ltime | log.Lmicroseconds | log.Lshortfile)
}
