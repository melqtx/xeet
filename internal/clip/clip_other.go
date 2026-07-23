//go:build !linux

package clip

import (
	"fmt"

	"golang.design/x/clipboard"
)

func Init() error { return clipboard.Init() }

func ReadImage() []byte { return clipboard.Read(clipboard.FmtImage) }

func ReadText() string { return string(clipboard.Read(clipboard.FmtText)) }

func WriteText(text string) error {
	if text == "" {
		return fmt.Errorf("cannot copy empty text")
	}
	clipboard.Write(clipboard.FmtText, []byte(text))
	return nil
}
