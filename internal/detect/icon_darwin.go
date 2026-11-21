//go:build cgo

package detect

/*
#cgo LDFLAGS: -framework AppKit -framework Foundation -framework CoreGraphics
#include <stdlib.h>
void *lowpath_app_icon(const char *path, int size, int *length);
*/
import "C"

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"unsafe"
)

func exeIcon(path string, size int) (image.Image, error) {
	if size < 1 || size > 1024 || !filepath.IsAbs(path) {
		return nil, errors.New("detect: invalid Mac icon request")
	}
	for dir := filepath.Clean(path); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if !strings.EqualFold(filepath.Ext(dir), ".app") {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, "Contents", "Info.plist")); err != nil {
			continue
		}
		name := C.CString(dir)
		defer C.free(unsafe.Pointer(name))
		var length C.int
		data := C.lowpath_app_icon(name, C.int(size), &length)
		if data != nil {
			defer C.free(data)
			return png.Decode(bytes.NewReader(C.GoBytes(data, length)))
		}
		break
	}
	return nil, fmt.Errorf("detect: no macOS application bundle icon: %w", errors.ErrUnsupported)
}
