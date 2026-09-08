// A credential-free TCP probe for the isolated Celln network acceptance test.
package main

import (
	"fmt"
	"net"
	"os"
	"time"
)

func main() {
	if len(os.Args) != 4 || (os.Args[1] != "allow" && os.Args[1] != "deny") {
		os.Exit(2)
	}
	for _, address := range os.Args[2:] {
		host, _, err := net.SplitHostPort(address)
		if err != nil || host != "10.89.0.1" {
			os.Exit(2)
		}
		connection, err := net.DialTimeout("tcp", address, 2*time.Second)
		if connection != nil {
			connection.Close()
		}
		if os.Args[1] == "allow" {
			if err != nil {
				fmt.Println("expected reachable endpoint")
				os.Exit(1)
			}
		} else {
			failure, ok := err.(net.Error)
			if !ok || !failure.Timeout() {
				fmt.Println("expected network timeout, not refusal or successful connection")
				os.Exit(1)
			}
		}
	}
	fmt.Println("both endpoints matched " + os.Args[1])
}
