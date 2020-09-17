// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2020 Datadog, Inc.

// +build secrets

package secrets

import (
	"syscall"
)

const (
	sigterm = syscall.CTRL_CLOSE_EVENT
)
