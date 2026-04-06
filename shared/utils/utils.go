// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package utils

func DerefOrZero[T any](ptr *T) T {
	if ptr == nil {
		var zero T
		return zero
	}
	return *ptr
}
