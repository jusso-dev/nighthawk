package linux

import "os"

func osGeteuidImpl() int { return os.Geteuid() }
