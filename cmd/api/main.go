// analytics-service API entry point. The process does one thing — hand
// control to lib/boot. Adding a new singleton / module = edit one fx.Provide
// in lib/boot/boot.go, NOT this file.
package main

import "github.com/zrafat80/quickbite/analytics-service/lib/boot"

func main() {
	boot.Run()
}
