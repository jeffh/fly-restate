package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.com/fly-apps/nats-cluster/pkg/supervisor"

	_ "embed"
)

func main() {
	restateVars, err := initRestateConfig()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	svisor := supervisor.New("flyrestate", 5*time.Minute)

	// svisor.AddProcess(
	// 	"exporter",
	// 	"nats-exporter -varz 'http://fly-local-6pn:8222'",
	// 	supervisor.WithRestart(0, 1*time.Second),
	// )

	svisor.AddProcess(
		"restate-server",
		"/usr/local/bin/restate-server --config-file=/etc/restate.toml",
		supervisor.WithRestart(0, 1*time.Second),
	)

	go watchRestateConfig(restateVars)

	svisor.StopOnSignal(syscall.SIGINT, syscall.SIGTERM)

	svisor.StartHttpListener()

	err = svisor.Run()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

type FlyEnv struct {
	MachineId      string
	Host           string
	AppName        string
	Region         string
	GatewayRegions []string
	MachineHosts   []string
	ServerName     string
	Timestamp      time.Time
	IPv4           net.IP
	IPv6           net.IP
}

//go:embed restate.toml.templ
var tmplRaw string

func watchRestateConfig(vars FlyEnv) {
	fmt.Println("Starting ticker")
	ticker := time.NewTicker(60 * time.Second)
	var lastReload time.Time

	go func() {
		for {
			for range ticker.C {
				newVars, err := restateConfigVars()

				if err != nil {
					fmt.Printf("error getting restate config vars: %v", err)
					continue
				}
				// if stringSlicesEqual(vars.GatewayRegions, newVars.GatewayRegions) {
				// 	// noop, nothing changed
				// 	//fmt.Println("No change in regions")
				// 	continue
				// }
				if stringSlicesEqual(vars.MachineHosts, newVars.MachineHosts) {
					// noop, nothing changed
					//fmt.Println("No change in regions")
					continue
				}

				cooloff := lastReload.Add(60 * 3 * time.Second)
				if time.Now().Before(cooloff) {
					fmt.Println("Regions changed, but cooloff period not expired")
					continue
				}

				err = writeRestateConfig(newVars)
				if err != nil {
					fmt.Printf("error writing restate config: %v", err)
				}

				fmt.Printf("Reloading restate: \n\t%v\n", newVars.MachineHosts)
				killProcessByName("restate-server", os.Stdout, os.Stderr)

				vars = newVars
				lastReload = time.Now()
			}
		}
	}()

	fmt.Println("ticker fn return")
}

func restateConfigVars() (FlyEnv, error) {
	host := "fly-local-6pn"
	appName := os.Getenv("FLY_APP_NAME")
	machineId := os.Getenv("FLY_ALLOC_ID")

	var ipv4 net.IP
	var ipv6 net.IP
	var regions []string
	var machineHosts []string
	// var err error

	if appName != "" {
		// regions, err = privnet.GetRegions(context.Background(), appName)
		// if err != nil {
		// 	// hardcode to current region to avoid failing to start the first instance
		// 	regions = []string{os.Getenv("FLY_REGION")}
		// 	// return FlyEnv{}, err
		// }

		ifaces, err := net.Interfaces()
		if err != nil {
			return FlyEnv{}, err
		}
		for _, iface := range ifaces {
			if iface.Name != "eth0" {
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				return FlyEnv{}, err
			}
			for _, addr := range addrs {
				ip, _, err := net.ParseCIDR(addr.String())
				if err != nil {
					return FlyEnv{}, err
				}
				if ip.To4() == nil {
					ipv6 = ip
				} else {
					ipv4 = ip
				}
			}
		}

		machineHosts, err = getMachines(context.Background(), appName)
		if err != nil {
			return FlyEnv{}, err
		}
	} else {
		// defaults for local exec
		host = "localhost"
		appName = "local"
		// regions = []string{"local"}
		machineHosts = []string{"localhost"}
	}

	if ipv4 == nil {
		ipv4 = net.ParseIP("0.0.0.0")
	}
	if ipv6 == nil {
		ipv6 = net.ParseIP("[::]")
	}

	// easier to compare
	sort.Strings(regions)

	region := os.Getenv("FLY_REGION")
	if region == "" {
		region = "local"
	}

	vars := FlyEnv{
		AppName: appName,
		Region:  region,
		// GatewayRegions: regions,
		Host:         host,
		MachineHosts: machineHosts,
		ServerName:   machineId,
		Timestamp:    time.Now(),
		IPv4:         ipv4,
		IPv6:         ipv6,
	}
	return vars, nil
}
func initRestateConfig() (FlyEnv, error) {
	vars, err := restateConfigVars()
	if err != nil {
		return vars, err
	}
	err = writeRestateConfig(vars)

	if err != nil {
		return vars, err
	}

	return vars, nil
}

func writeRestateConfig(vars FlyEnv) error {
	tmpl, err := template.New("conf").Parse(tmplRaw)

	if err != nil {
		return err
	}

	f, err := os.Create("/etc/restate.toml")

	if err != nil {
		return err
	}

	err = tmpl.Execute(f, vars)

	if err != nil {
		return err
	}

	return nil
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

func ipAddrsEqual(a, b []net.IPAddr) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v.String() != b[i].String() {
			return false
		}
	}
	return true
}

// KillProcessByName finds the PID(s) of a process by its name using `pidof`
// and then attempts to kill it/them using the `kill` command.
// It returns an error if the commands fail or if no processes are found.
//
// Note: This function assumes `pidof` and `kill` are available in the system's PATH.
func killProcessByName(processName string, osStdout, osStderr io.Writer) error {
	if processName == "" {
		return fmt.Errorf("process name cannot be empty")
	}

	// 1. Find the PID(s) using pidof
	cmdPidof := exec.Command("pidof", processName)
	var stdout, stderr bytes.Buffer
	cmdPidof.Stdout = &stdout
	cmdPidof.Stderr = &stderr

	err := cmdPidof.Run()

	// pidof returns exit code 1 if no processes are found.
	// Other non-zero exit codes or errors indicate a problem running pidof.
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// pidof returns 1 when no processes are found. This is not an error
			// for our purpose; it just means there's nothing to kill.
			if exitErr.ExitCode() == 1 {
				fmt.Printf("No process found with name '%s'\n", processName)
				return nil // Success, nothing to kill
			}
			// Other exit codes from pidof are actual errors
			return fmt.Errorf("failed to run pidof for '%s' (exit code %d): %s", processName, exitErr.ExitCode(), stderr.String())
		}
		// Handle other types of errors (e.g., command not found)
		return fmt.Errorf("failed to run pidof for '%s': %w %s", processName, err, stderr.String())
	}

	// Get the PIDs from pidof's output
	pidOutput := strings.TrimSpace(stdout.String())
	if pidOutput == "" {
		// Should ideally be caught by the ExitError check above, but double-check.
		fmt.Printf("pidof returned empty output for '%s'. No processes to kill.\n", processName)
		return nil
	}

	// pidof outputs space-separated PIDs
	pids := strings.Fields(pidOutput)

	fmt.Printf("Found PIDs for '%s': %s. Attempting to kill...\n", processName, strings.Join(pids, ", "))

	// 2. Kill the process(es) using kill
	// The kill command takes PIDs as arguments
	killArgs := append([]string{}, pids...) // Create a new slice starting with PIDs

	cmdKill := exec.Command("kill", killArgs...)
	cmdKill.Stdout = osStdout
	cmdKill.Stderr = osStderr

	err = cmdKill.Run()
	if err != nil {
		// kill might fail for various reasons (e.g., permission denied, invalid PID if pidof messed up)
		return fmt.Errorf("failed to kill process(es) with PIDs %s: %w", strings.Join(pids, ", "), err)
	}

	fmt.Printf("Successfully sent termination signal to process(es) with PIDs %s\n", strings.Join(pids, ", "))

	return nil // Success
}

func getMachines(ctx context.Context, appName string) ([]string, error) {
	nameserver := os.Getenv("FLY_NAMESERVER")
	if nameserver == "" {
		nameserver = "fdaa::3"
	}
	nameserver = net.JoinHostPort(nameserver, "53")
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: 1 * time.Second,
			}
			return d.DialContext(ctx, "udp6", nameserver)
		},
	}
	hostname := fmt.Sprintf("vms.%s.internal", appName)
	raw, err := r.LookupTXT(ctx, hostname)

	if err != nil {
		return nil, err
	}

	machines := make([]string, 0)

	for _, r := range raw {
		for _, machine := range strings.Split(r, ",") {
			parts := strings.SplitN(machine, " ", 2)
			if len(parts) == 2 {
				machines = append(machines, fmt.Sprintf("%s.vm.%s.internal", parts[0], appName))
			}
		}
	}

	return machines, nil
}
