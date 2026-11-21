//go:build cgo

package platform

import (
	"lowpath/internal/capture"
	"lowpath/internal/owner"
)

func nativeCapture(owner.Resolver) capture.Backend { return capture.TransparentProxy{} }
