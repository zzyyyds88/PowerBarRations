package common

import "os"

func initNodeNameIdentity() {
	if envNodeName := os.Getenv("NODE_NAME"); envNodeName != "" {
		NodeName = envNodeName
		NodeNameSource = NodeNameSourceManual
		NodeNameManuallyConfigured = true
		return
	}

	hostname, _ := os.Hostname()
	NodeName = hostname
	NodeNameSource = NodeNameSourceHostname
	NodeNameManuallyConfigured = false
}
