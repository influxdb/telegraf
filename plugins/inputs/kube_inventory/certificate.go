package kube_inventory

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/influxdata/telegraf"
)

func collectSecrets(ctx context.Context, acc telegraf.Accumulator, ki *KubernetesInventory) {
	list, err := ki.client.getTLSSecrets(ctx)
	if err != nil {
		acc.AddError(err)
		return
	}
	for _, i := range list.Items {
		gatherCertificates(i, acc)
	}
}

func getFields(cert *x509.Certificate, now time.Time) map[string]interface{} {
	age := int(now.Sub(cert.NotBefore).Seconds())
	expiry := int(cert.NotAfter.Sub(now).Seconds())
	startdate := cert.NotBefore.Unix()
	enddate := cert.NotAfter.Unix()

	fields := map[string]interface{}{
		"age":       age,
		"expiry":    expiry,
		"startdate": startdate,
		"enddate":   enddate,
	}

	return fields
}

func getTags(cert *x509.Certificate) map[string]string {
	tags := map[string]string{
		"common_name":          cert.Subject.CommonName,
		"signature_algorithm":  cert.SignatureAlgorithm.String(),
		"public_key_algorithm": cert.PublicKeyAlgorithm.String(),
	}
	tags["issuer_common_name"] = cert.Issuer.CommonName

	san := append(cert.DNSNames, cert.EmailAddresses...)
	for _, ip := range cert.IPAddresses {
		san = append(san, ip.String())
	}
	for _, uri := range cert.URIs {
		san = append(san, uri.String())
	}
	tags["san"] = strings.Join(san, ",")

	return tags
}

func gatherCertificates(r corev1.Secret, acc telegraf.Accumulator) {
	now := time.Now()

	for resourceName, val := range r.Data {
		if resourceName != "tls.crt" {
			continue
		}
		block, _ := pem.Decode(val)
		if block == nil {
			return
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return
		}
		fields := getFields(cert, now)
		tags := getTags(cert)
		tags["name"] = r.Name
		tags["namespace"] = r.Namespace
		opts := x509.VerifyOptions{
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
		}
		_, err = cert.Verify(opts)
		if err == nil {
			tags["verification"] = "valid"
			fields["verification_code"] = 0
		} else {
			tags["verification"] = "invalid"
			fields["verification_code"] = 1
		}
		acc.AddFields(certificateMeasurement, fields, tags)
	}
}
