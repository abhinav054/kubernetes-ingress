// Copyright 2026 HAProxy Technologies
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package handler

import (
	"testing"

	"github.com/haproxytech/client-native/v6/models"
	"github.com/stretchr/testify/require"
)

func TestEnableFrontendTLS(t *testing.T) {
	frontend := &models.Frontend{
		Binds: map[string]models.Bind{
			"v4": {
				Name: "v4",
				BindParams: models.BindParams{
					Alpn: "h2,http/1.1",
				},
			},
			"v6": {
				Name: "v6",
				BindParams: models.BindParams{
					Ssl:            true,
					SslCertificate: "/custom/certs",
				},
			},
		},
	}

	require.True(t, enableFrontendTLS(frontend, "/etc/haproxy/certs/frontend"))

	v4 := frontend.Binds["v4"]
	require.True(t, v4.Ssl)
	require.Equal(t, "/etc/haproxy/certs/frontend", v4.SslCertificate)
	require.Equal(t, "h2,http/1.1", v4.Alpn)

	v6 := frontend.Binds["v6"]
	require.True(t, v6.Ssl)
	require.Equal(t, "/custom/certs", v6.SslCertificate)

	require.False(t, enableFrontendTLS(frontend, "/etc/haproxy/certs/frontend"))
}

func TestRemoveTLSCRSSLFrontUses(t *testing.T) {
	frontend := &models.Frontend{
		SSLFrontUses: models.SSLFrontUses{
			{
				Certificate: "/managed.pem",
				Metadata: map[string]interface{}{
					tlsCROwnerMetadata: "default/domain-tls",
				},
			},
			{Certificate: "/user-configured.pem"},
		},
	}

	require.True(t, removeTLSCRSSLFrontUses(frontend))
	require.Len(t, frontend.SSLFrontUses, 1)
	require.Equal(t, "/user-configured.pem", frontend.SSLFrontUses[0].Certificate)
	require.False(t, removeTLSCRSSLFrontUses(frontend))
}
