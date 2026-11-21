package managed

import (
	"context"
	"errors"
	"net"
	"slices"
	"testing"
)

func TestTryUplinksRetriesPhysicalInterfacesOnly(t *testing.T) {
	unreachable := errors.New("unreachable")
	candidates := []net.Interface{{Name: "en9", Index: 19}, {Name: "en0", Index: 11}}
	var tried []int
	result, err := tryUplinks(context.Background(), candidates, func(_ context.Context, adapter net.Interface) (string, error) {
		tried = append(tried, adapter.Index)
		if adapter.Name == "en9" {
			return "", unreachable
		}
		return adapter.Name, nil
	})
	if err != nil || result != "en0" || !slices.Equal(tried, []int{19, 11}) {
		t.Fatalf("result = %q, interfaces = %v, error = %v", result, tried, err)
	}
	tried = nil
	_, err = tryUplinks(context.Background(), candidates, func(_ context.Context, adapter net.Interface) (int, error) {
		tried = append(tried, adapter.Index)
		return 0, unreachable
	})
	if !errors.Is(err, unreachable) || !slices.Equal(tried, []int{19, 11}) {
		t.Fatalf("failed physical paths must not fall back to the unbound system route: %v, %v", tried, err)
	}
}
