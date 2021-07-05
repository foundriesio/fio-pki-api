package main

import (
	"bytes"
	"flag"
	"io/ioutil"
	"encoding/json"
	"net/http"
	"fmt"
)

type (
	FactoryCerts struct {
		RootCrt string `json:"root-crt"`
		DeviceCaCrt   string `json:"ca-crt"`
		ServerCrt  string `json:"tls-crt"`
	}
)

func main() {
	factory := flag.String("factory", "", "Factory")
	token := flag.String("token", "", "Factory access token")
	rootCrt := flag.String("root-cert", "", "root CA cert")
	fleetCA := flag.String("fleet-ca-cert", "", "fleet CA cert")
	serverCert := flag.String("server-cert", "", "A server/Device Gateway certificate")
	flag.Parse()

	var fc FactoryCerts

	buf, err := ioutil.ReadFile(*rootCrt)
	if err != nil {
		panic(err)
	}
	fc.RootCrt = string(buf)

	buf, err = ioutil.ReadFile(*fleetCA)
	if err != nil {
		panic(err)
	}
	fc.DeviceCaCrt = string(buf)

	buf, err = ioutil.ReadFile(*serverCert)
	if err != nil {
		panic(err)
	}
	fc.ServerCrt = string(buf)

	url := "https://api.foundries.io/ota/factories/" + *factory + "/certs/"

	data, err := json.Marshal(fc)
	if err != nil {
		panic(err)
	}

	req, err := http.NewRequest(http.MethodPatch, url, bytes.NewBuffer(data))
	if err != nil {
		panic(err)
	}

	req.Header.Set("OSF-TOKEN", *token)
	req.Header.Set("Content-Type", "application/json")


	res, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}

	rd, err := ioutil.ReadAll(res.Body)
	if err != nil {
		panic(err)
	}
	fmt.Printf("status: %s\n%s\n", res.Status, string(rd))
}
