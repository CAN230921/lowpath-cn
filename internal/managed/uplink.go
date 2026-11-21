package managed

import (
	"context"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
)

type uplinkSelector func() ([]net.Interface, error)

func selectUplink(name string) (uplinkSelector, error) {
	switch name {
	case "":
		return func() ([]net.Interface, error) { return []net.Interface{{}}, nil }, nil
	case "auto":
		return physicalUplinks, nil
	default:
		if _, err := net.InterfaceByName(name); err != nil {
			return nil, err
		}
		return func() ([]net.Interface, error) {
			adapter, err := net.InterfaceByName(name)
			if err != nil {
				return nil, err
			}
			return []net.Interface{*adapter}, nil
		}, nil
	}
}

func socketControl(index int) func(string, string, syscall.RawConn) error {
	if index == 0 {
		return nil
	}
	return func(_, _ string, socket syscall.RawConn) error { return bindInterface(socket, index) }
}

// tryUplinks attempts only interfaces chosen for this connection. In auto
// mode there is no unbound fallback that could send it through a TUN.
func tryUplinks[T any](ctx context.Context, candidates []net.Interface, connect func(context.Context, net.Interface) (T, error)) (T, error) {
	var zero T
	if len(candidates) == 0 {
		return zero, errors.New("no usable gateway interface")
	}
	var failures error
	for i, adapter := range candidates {
		if ctx.Err() != nil {
			return zero, errors.Join(failures, ctx.Err())
		}
		attempt, cancel := ctx, func() {}
		if i+1 < len(candidates) {
			attempt, cancel = context.WithTimeout(ctx, 2*time.Second)
		}
		connected, err := connect(attempt, adapter)
		cancel()
		if err == nil {
			return connected, nil
		}
		if adapter.Name != "" {
			err = fmt.Errorf("%s: %w", adapter.Name, err)
		}
		failures = errors.Join(failures, err)
	}
	return zero, failures
}
