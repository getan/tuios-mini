//go:build !unix

package workspace

import "os"

func drainLateKeyboardReply(*os.File) {}
