package main

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type Targets struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels"`
}

var ignoredNodes []string

type Vms struct {
	Vm []Vm
}

type Vm struct {
	Fqdn    string
	Id      string
	Domain  Domain
	Cluster Cluster
}

type Domain struct {
	Name string
}

type Hosts struct {
	Host []Host
}

type Host struct {
	Address string
	Cluster Cluster
}

type Cluster struct {
	Id string
}

type Config struct {
	Target           string
	URL              string
	User             string
	Password         string
	NoVerify         bool
	EngineCA         string
	UpdateInterval   int
	TargetPort       int
	IgnoredNodesFile string
}

func main() {
	target := flag.String("output", "engine-hosts.json", "target for the configuration file")
	engineURL := flag.String("engine-url", "https://localhost:8443", "Engine URL")
	engineUser := flag.String("engine-user", "admin@internal", "Engine user")
	enginePassword := flag.String("engine-password", "", "Engine password. Consider using ENGINE_PASSWORD environment variable to set this")
	noVerify := flag.Bool("no-verify", false, "Don't verify the engine certificate")
	engineCa := flag.String("engine-ca", "/etc/pki/ovirt-engine/ca.pem", "Path to engine ca certificate")
	updateInterval := flag.Int("update-interval", 60, "Update intervall for host discovery in seconds")
	targetPort := flag.Int("host-port", 9000, "Port where Prometheus metrics are exposed on the hosts")
	ignoredNodesFile := flag.String("ignored-nodes-file", "ignored-nodes.txt", "File with list of hosts to be ignored")
	flag.Parse()
	if *enginePassword == "" {
		*enginePassword = os.Getenv("ENGINE_PASSWORD")
	}
	config := Config{Target: *target,
		URL:              *engineURL,
		User:             *engineUser,
		Password:         *enginePassword,
		NoVerify:         *noVerify,
		EngineCA:         *engineCa,
		UpdateInterval:   *updateInterval,
		TargetPort:       *targetPort,
		IgnoredNodesFile: *ignoredNodesFile,
	}

	file, err := os.Open(config.IgnoredNodesFile)
	if err != nil {
		log.Print(err)
	}
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		ignoredNodes = append(ignoredNodes, scanner.Text())
	}

	if !strings.HasPrefix(config.URL, "https") {
		log.Fatal("Only URLs starting with 'https' are supported")
	}
	if config.Password == "" {
		log.Fatal("No engine password supplied")
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: config.NoVerify,
	}
	if !config.NoVerify {
		roots := x509.NewCertPool()
		ok := roots.AppendCertsFromPEM(readFile(config.EngineCA))
		if !ok {
			log.Panic("Could not load root CA certificate")
		}

		tlsConfig.RootCAs = roots
	}
	tlsConfig.BuildNameToCertificate()
	transport := &http.Transport{TLSClientConfig: tlsConfig}
	client := &http.Client{Transport: transport}
	Discover(client, &config)
}
func isIgnoredNode(node string) bool {
	set := make(map[string]struct{}, len(ignoredNodes))
	for _, s := range ignoredNodes {
		set[s] = struct{}{}
	}

	_, ok := set[node]
	return ok
}

func Discover(client *http.Client, config *Config) {
	req, err := http.NewRequest("GET", config.URL+"/ovirt-engine/api/vms", nil)
	check(err)
	req.Header.Add("Accept", "application/json")
	req.SetBasicAuth(config.User, config.Password)
	data := make(chan []byte)
	done := writeTargets(config.Target, MapToTarget(config.TargetPort, ParseJson(data)))
	go func() {
		defer close(data)
		for {
			res, err := client.Do(req)
			if err != nil {
				log.Print(err)
				time.Sleep(time.Duration(config.UpdateInterval) * time.Second)
				continue
			}
			hosts, err := ioutil.ReadAll(res.Body)
			res.Body.Close()
			if err != nil {
				log.Print(err)
				time.Sleep(time.Duration(config.UpdateInterval) * time.Second)
				continue
			}
			data <- hosts
			time.Sleep(time.Duration(config.UpdateInterval) * time.Second)
		}
	}()
	<-done
}

func ParseJson(data chan []byte) chan *Vms {
	hostsChan := make(chan *Vms)
	go func() {
		defer close(hostsChan)
		for msg := range data {
			hosts := new(Vms)
			err := json.Unmarshal(msg, hosts)
			if err != nil {
				log.Print(err)
				continue
			}
			hostsChan <- hosts
		}
	}()
	return hostsChan
}

func MapToTarget(targetPort int, hosts chan *Vms) chan []*Targets {
	targetsChan := make(chan []*Targets)
	go func() {
		defer close(targetsChan)
		for msg := range hosts {
			targetMap := make(map[string]*Targets)
			var targets []*Targets
			for _, host := range msg.Vm {
				if !isIgnoredNode(host.Fqdn) {
					if (len(host.Fqdn) == 0) || !(strings.Contains(host.Fqdn, ".")) { //Remove templates and VMs with no fqdn. Better way to do this?
					} else if value, ok := targetMap[host.Cluster.Id]; ok {
						value.Targets = append(value.Targets, host.Fqdn+":"+strconv.Itoa(targetPort))
					} else {
						targetMap[host.Cluster.Id] = &Targets{
							Labels:  map[string]string{"cluster": host.Cluster.Id},
							Targets: []string{host.Fqdn + ":" + strconv.Itoa(targetPort)}}
						targets = append(targets, targetMap[host.Cluster.Id])
					}
				}
			}
			targetsChan <- targets
		}
	}()
	return targetsChan
}

func writeTargets(fileName string, targets chan []*Targets) chan error {
	done := make(chan error)
	go func() {
		defer close(done)
		for msg := range targets {
			if len(msg) == 0 {
				err := ioutil.WriteFile(fileName+".new", []byte("[]"), 0644)
				if err != nil {
					log.Print(err)
					continue
				}
			} else {
				data, _ := json.MarshalIndent(msg, "", "  ")
				data = append(data, '\n')
				err := ioutil.WriteFile(fileName+".new", data, 0644)
				if err != nil {
					log.Print(err)
					continue
				}
			}
			os.Rename(fileName+".new", fileName)
		}
	}()
	return done
}

func check(e error) {
	if e != nil {
		log.Fatal(e)
	}
}

func readFile(fileName string) []byte {
	bytes, err := ioutil.ReadFile(fileName)
	check(err)
	return bytes
}
