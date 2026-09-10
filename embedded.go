// Package xtoolrunner contains the resources compiled into the distributable
// controller binary.
package xtoolrunner

import _ "embed"

//go:embed resources/guest/provision.sh
var ProvisionGuestScript string

//go:embed resources/guest/run.sh
var RunRunnerScript string
