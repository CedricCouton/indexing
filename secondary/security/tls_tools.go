//  Copyright 2023-Present Couchbase, Inc.
//
//  Use of this software is governed by the Business Source License included
//  in the file licenses/BSL-Couchbase.txt.  As of the Change Date specified
//  in that file, in accordance with the Business Source License, use of this
//  software will be governed by the Apache License, Version 2.0, included in
//  the file licenses/APL2.txt.

package security

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/couchbase/indexing/secondary/logging"
)

//
// Indexing tools setting.
// Caution: These functions need to be used only for indexing tools & not couchbase-server processes.
//
type ToolsConfig struct {
	user               string
	passwd             string
	cert               string
	insecureSkipVerify bool
	useConfig          bool
}

func SetToolsConfig(user, passwd, cert string, insecureSkipVerify bool, useConfig bool) error {
	if user == "" || passwd == "" {
		err := fmt.Errorf("non empty credentials required")
		return err
	}
	if cert == "" && insecureSkipVerify == false {
		err := fmt.Errorf("certificate file missing for SSL verification")
		return err
	}
	_toolsConfig.user = user
	_toolsConfig.passwd = passwd
	_toolsConfig.cert = cert
	_toolsConfig.insecureSkipVerify = insecureSkipVerify
	_toolsConfig.useConfig = true
	return nil
}

func GetToolsCreds() (string, string) {
	return _toolsConfig.user, _toolsConfig.passwd
}

func GetToolsTLSConf() (string, bool, bool) {
	return _toolsConfig.cert, _toolsConfig.insecureSkipVerify, _toolsConfig.useConfig
}

func IsToolsConfigUsed() bool {
	return _toolsConfig.useConfig
}

func getWithAuthInternalTools(u string, params *RequestParams, eTag string) (*http.Response, error) {

	var url *url.URL
	var err error

	url, err = GetURL(u)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	defer func() {
		logging.Verbosef("getWithAuthInternalTools: url %v elapsed %v", url.String(), time.Now().Sub(start))
	}()

	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return nil, err
	}

	if eTag != "" {
		req.Header.Set("If-None-Match", eTag) // common.HTTP_KEY_ETAG_REQUEST
	}

	if params != nil && params.UserAgent != "" {
		req.Header.Add("User-agent", userAgentPrefix+params.UserAgent)
	}

	var client *http.Client

	tlsConfig := tls.Config{}
	if _toolsConfig.cert == "" || _toolsConfig.insecureSkipVerify {
		tlsConfig.InsecureSkipVerify = true
	} else {
		caCert, err := ioutil.ReadFile(_toolsConfig.cert)
		if err != nil {
			return nil, err
		}
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)
		tlsConfig.RootCAs = caCertPool
	}

	client = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tlsConfig,
		},
	}
	req.SetBasicAuth(_toolsConfig.user, _toolsConfig.passwd)

	if params != nil && params.Timeout >= time.Duration(0) {
		client.Timeout = params.Timeout
	}

	return client.Do(req)
}

func GetURLTools(u string) (*url.URL, error) {
	if !strings.HasPrefix(u, "https://") {
		u = "https://" + u
	}
	parsedUrl, err := url.Parse(u)
	if err != nil {
		return nil, err
	}
	parsedUrl.Host, _, _, err = EncryptPortFromAddr(parsedUrl.Host)
	if err != nil {
		return nil, err
	}
	parsedUrl.Scheme = "https"
	return parsedUrl, nil
}

func setupClientTLSConfigTools(host string) (*tls.Config, error) {

	cert, insecureSkipVerify, _ := GetToolsTLSConf()

	// Setup  TLSConfig
	tlsConfig := &tls.Config{}

	if cert != "" && !insecureSkipVerify {
		caCert, err := ioutil.ReadFile(cert)
		if err != nil {
			logging.Errorf("certificate read failed, err: %v", err)
			return nil, err
		}
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)
		tlsConfig.RootCAs = caCertPool
	}
	if insecureSkipVerify {
		tlsConfig.InsecureSkipVerify = true
	} else {
		tlsConfig.ServerName = host
	}

	return tlsConfig, nil
}

var _toolsConfig ToolsConfig = ToolsConfig{user: "", passwd: "", cert: "", insecureSkipVerify: false, useConfig: false}
