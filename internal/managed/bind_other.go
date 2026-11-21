//go:build !darwin

package managed

import (
	"errors"
	"net"
	"syscall"
)

func bindInterface(syscall.RawConn, int) error { return errors.ErrUnsupported }

func physicalUplinks() ([]net.Interface, error) { return nil, errors.ErrUnsupported }
