//go:build !darwin && !windows

package sysproxy

import (
	"errors"
	"time"
)

const supported = false

var errUnsupported = errors.New("当前平台还不支持自动设置系统代理")

func Service() (string, error) { return "", errUnsupported }

func Current() (State, error) { return State{}, nil }

func Enable(port int) error { return errUnsupported }

func Disable() error { return errUnsupported }

func Apply(server string, port int) error { return errUnsupported }

func SetTimeout(time.Duration) {}

func Restore(st State) error { return errUnsupported }
