//go:build !darwin || !cgo

package main

import "errors"

func isRouteController() bool   { return false }
func runRouteController() error { return errors.ErrUnsupported }
