package utils

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io/ioutil"

	"github.com/golang/glog"
)

// GenServerTLSConfigFromCAFile creates a tls Config with ClientCAs.
func GenServerTLSConfigFromCAFile(caFile string) (*tls.Config, error) {
	b, err := ioutil.ReadFile(caFile)
	if err != nil {
		glog.Errorln("read ca cert file error", err, caFile)
		return nil, err
	}

	return GenServerTLSConfigWithCA(b)
}

// GenServerTLSConfigWithCA creates a tls Config with ClientCAs.
func GenServerTLSConfigWithCA(clientCACert []byte) (*tls.Config, error) {
	clientCertPool := x509.NewCertPool()
	ok := clientCertPool.AppendCertsFromPEM(clientCACert)
	if !ok {
		return nil, errors.New("failed to append clientCACert")
	}

	tlsConfig := &tls.Config{
		// Reject any TLS certificate that cannot be validated
		ClientAuth: tls.RequireAndVerifyClientCert,
		// Ensure that we only use our "CA" to validate certificates
		ClientCAs: clientCertPool,
		// PFS because we can but this will reject client with RSA certificates
		//CipherSuites: []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
		// Force it server side
		//PreferServerCipherSuites: true,
		// TLS 1.2 because we can
		//MinVersion: tls.VersionTLS12,
	}

	tlsConfig.BuildNameToCertificate()
	return tlsConfig, nil
}

// GenClientTLSConfig creates the tls Config for client
func GenClientTLSConfig(caFile string, certFile string, keyFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		glog.Errorln("LoadX509KeyPair error", err, "cert file", certFile, "key file", keyFile)
		return nil, err
	}

	// Load our CA certificate
	clientCACert, err := ioutil.ReadFile(caFile)
	if err != nil {
		glog.Errorln("read ca file error", err, caFile)
		return nil, err
	}

	clientCertPool := x509.NewCertPool()
	clientCertPool.AppendCertsFromPEM(clientCACert)

	tlsConf := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      clientCertPool,
	}

	tlsConf.BuildNameToCertificate()
	return tlsConf, nil
}
