#!/bin/bash

# Generate self-signed TLS certificates for FlashDB
# This script creates server.crt and server.key files
# Warning: These are for testing purposes only

set -e

echo "🔐 Generating self-signed TLS certificates for FlashDB..."

# Create certificate configuration
cat > cert.conf << EOF
[ req ]
default_bits       = 2048
default_keyfile    = server.key
digest             = sha256
prompt             = no
default_md         = sha256
distinguished_name = dn

[ dn ]
C                  = US
ST                 = California
L                  = San Francisco
O                  = FlashDB
OU                 = Engineering
CN                 = localhost

[ v3_ext ]
authorityKeyIdentifier = keyid,issuer:always
basicConstraints = CA:TRUE
keyUsage = digitalSignature, nonRepudiation, keyEncipherment, dataEncipherment, keyAgreement, keyCertSign
subjectAltName = @alt_names

[ alt_names ]
DNS.1 = localhost
IP.1 = 127.0.0.1
EOF

# Generate private key
openssl genrsa -out server.key 2048

# Generate certificate signing request (CSR)
openssl req -new -key server.key -out server.csr -config cert.conf

# Generate self-signed certificate
openssl x509 -req -in server.csr -signkey server.key -days 3650 -out server.crt -extfile cert.conf -extensions v3_ext

# Cleanup temporary files
rm -f cert.conf server.csr

echo "✅ Certificates generated successfully!"
echo "📄 server.crt (certificate)"
echo "🔑 server.key (private key)"
echo ""
echo "Usage:"
echo "1. Start FlashDB with TLS enabled:"
echo "   ./flashdb --tls --cert-file server.crt --key-file server.key"
echo ""
echo "2. Update your config.yaml:"
echo "   tls: true"
echo "   cert-file: \"server.crt\""
echo "   key-file: \"server.key\""
echo ""
echo "⚠️  These certificates are for testing purposes only. For production, use certificates from a trusted CA."