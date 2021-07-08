# How to set a lock for your Factory and secure access to it

## REST API

### Prerequisite
`openssl` `curl` `jq`

### Get access token

Go to https://app.foundries.io/settings/tokens/ and generate a new API token. Details on the API access can be found on https://docs.foundries.io/latest/reference-manual/factory/api-access.html?highlight=api%20tokens#api-access.
What permissions are required?

### Export the required environment variables
```
export TOKEN=<token>
export FACTORY=<factory>
```

### Check current Factory's certs & key.
Just after Factory creation so-called shared certs and key are applied to Factory.
```
curl -s -H "OSF-TOKEN: $TOKEN" https://api.foundries.io/ota/factories/$FACTORY/certs/ | jq
{
  "tls-crt": null,
  "ca-crt": null,
  "root-crt": null
}
```
If they already set (not null) then ask Foundries's support to delete them. 

### Generate PKI for your Factory

#### Generate your Factory PKI Root CA, i.e. a root CA private key and a self-signed root CA certificate.
```
mkdir factory-root
```
```
openssl ecparam -genkey -name secp521r1 -noout -out factory-root/factory-root.key
```
```
openssl req  -new -x509 -days 3560 -key factory-root/factory-root.key -subj "/CN=Factory Root CA/OU=${FACTORY}" -addext "keyUsage=critical,keyCertSign" -out factory-root/factory-root-ca.crt
```

#### Initiate Factory PKI creation

```
curl -s -X POST -H "Content-Type: application/json" -H "OSF-TOKEN: $TOKEN" "https://api.foundries.io/ota/factories/${FACTORY}/certs/" | jq . > factory-root/factory_certs.json
```

Extract Device Gateway's CSR (aka TLS CSR). The backend owns the private key the CSR derives from.
It should be signed with Factory root CA key & cert and uploaded to the backend.
It will be used by Device Gateway for setting up of mutual TLS, specifically for Device Gateway identity verification by a device,
and for generation of a symmetric TLS session key.
```
mkdir device-gateway
```
```
cat factory-root/factory_certs.json | jq -r '."tls-csr"' > device-gateway/device-gateway.csr
```

Extract Fleet CA CSR (aka online Fleet CA). The backend owns the private key the given CSR derives from.
It should be signed with Factory root CA key & cert and uploaded to the backend.
The backend will use them (the private key it owns and the signed Fleet CA cert) for a device CSR signing. (lmp-device-register).
```
mkdir fleet-ca
```
```
cat factory-root/factory_certs.json | jq -r '."ca-csr"' > fleet-ca/fleet-online-ca.csr
```

#### Sign Device Gateway CSR
openssl x509 does NOT copy CSR extensions thus we need to create a file with the x509v3 extensions required
for Device Gateway certificate.
First of all, a SAN list has to be extracted from the Device Gateway CSR (tls-csr) returned by the backend. 
```
echo "subjectAltName=$(openssl req -text -noout -in device-gateway/device-gateway.csr | grep DNS | tr -d " ")" > device-gateway/device-gateway.ext
``` 
Then, the `keyUsage` values should be set, as result `device-gateway/device-gateway.ext` should contain the following
```
subjectAltName=<SAN from CSR>
keyUsage=digitalSignature,keyEncipherment,keyAgreement
extendedKeyUsage=serverAuth
``` 

Sign Device Gateway CSR
```
openssl x509 -req -days 3650 -in device-gateway/device-gateway.csr -CAcreateserial -extfile device-gateway/device-gateway.ext -CAkey factory-root/factory-root.key -CA factory-root/factory-root-ca.crt -out device-gateway/device-gateway.crt
```

#### Sign online Fleet CA CSR
openssl x509 does NOT copy CSR extensions thus we need to create a file with the x509v3 extensions required
for Online Device/Fleet CA CSR returned by the backedn (ca-csr).
The extension file `fleet-ca/fleet-online-ca.ext` should contain the following
```
keyUsage=keyCertSign
basicConstraints=CA:true
```
Sign Online Device/Fleet CSR
```
openssl x509 -req -days 3650 -in fleet-ca/fleet-online-ca.csr -CAcreateserial -extfile fleet-ca/fleet-online-ca.ext -CAkey factory-root/factory-root.key -CA factory-root/factory-root-ca.crt -out fleet-ca/fleet-online-ca.crt
```

### Upload the produced PKI to the OTA backend
```
./patch-certs -factory ${FACTORY} -token ${TOKEN} -root-cert factory-root/factory-root-ca.crt -fleet-ca-cert fleet-ca/fleet-online-ca.crt -server-cert device-gateway/device-gateway.crt
```

### Create device CSR, and sign it with Online Device/Fleet CA by using [lmp-device-register](https://github.com/foundriesio/lmp-device-register)
```
mkdir -p devices/online-device
```
```
export DEVICE_FACTORY=$FACTORY
$LMP_DEV_REG -d $PWD/devices/online-device -T $TOKEN --start-daemon 0
```

#### Check the resultant certificates and a device key
Get an URL of the Factory Device Gateway
```
cat devices/online-device/sota.toml | grep repo_server
```
Check if you can fetch Targets
```
curl --cacert devices/online-device/root.crt --cert devices/online-device/client.pem --key devices/online-device/pkey.pem <repo_server>/targets.json | jq
```

Get an URL of the Factory OSTree Server
```
cat devices/online-device/sota.toml | grep ostree_server
```
Check if you can fetch Factory OSTree repo
```
curl --cacert devices/online-device/root.crt --cert devices/online-device/client.pem --key devices/online-device/pkey.pem <ostree_server>/config
```

### Create offline Device/Fleet CA

Offline key
```
openssl ecparam -genkey -name secp521r1 -noout -out fleet-ca/fleet-offline.key
```
Fleet CA CSR conf (`fleet-ca/fleet-offline-ca.conf`)
```
[req]

prompt = no
days=3650
distinguished_name = req_dn

[req_dn]

# fio prefix is mandatory
commonName="fio-Fleet-offline-CA"
organizationalUnitName="${FACTORY}"
```

Fleet CA extension (`fleet-ca/fleet-offline-ca.ext`)
```
keyUsage=keyCertSign
basicConstraints=CA:true
```

Generate Offline Fleet CSR
```
openssl req -new -config fleet-ca/fleet-offline-ca.conf -key fleet-ca/fleet-offline.key -out fleet-ca/fleet-offline-ca.csr
```
Sign Offline Fleet CSR
```
openssl x509 -req -in fleet-ca/fleet-offline-ca.csr -CAcreateserial -extfile fleet-ca/fleet-offline-ca.ext -CAkey factory-root/factory-root.key -CA factory-root/factory-root-ca.crt -out fleet-ca/fleet-offline-ca.crt
```
Merge the online and offline Device/Fleet certificates into a certificate bundle/chain
```
cat fleet-ca/fleet-offline-ca.crt fleet-ca/fleet-online-ca.crt > fleet-ca/fleet-ca-bundle.crt
```

Update the factory certificates with the offline FLeet certificate
```
./patch-certs -factory ${FACTORY} -token ${TOKEN} -root-cert factory-root/factory-root-ca.crt -fleet-ca-cert fleet-ca/fleet-ca-bundle.crt -server-cert device-gateway/device-gateway.crt
```

### Create Offline device
```
mkdir devices/offline-device
```
```
openssl ecparam -genkey -name secp521r1 -noout -out devices/offline-device/pkey.pem
```

Set offline Device certificate config (`devices/offline-device/device-cert.conf`)
```
[req]

prompt = no
days=3650
distinguished_name = req_dn

[req_dn]

commonName="offline-device-<uuid>"
organizationalUnitName="${FACTORY}"
``` 

Set offline Device certificate extensions (`devices/offline-device/device-cert.ext`)
```
keyUsage=critical,digitalSignature,keyAgreement
extendedKeyUsage=critical,clientAuth
```

Generate CSR
```
openssl req -new -config devices/offline-device/device-cert.conf -key devices/offline-device/pkey.pem -out devices/offline-device/device-cert.csr
```

Sign CSR and produce offline Device certificate
```
openssl x509 -req -in devices/offline-device/device-cert.csr -CAcreateserial -extfile devices/offline-device/device-cert.ext -CAkey fleet-ca/fleet-offline.key -CA fleet-ca/fleet-offline-ca.crt -out devices/offline-device/client.pem
``` 

#### Check offline Device CA and Device certificates

```
curl --cacert factory-root/factory-root-ca.crt --cert devices/offline-device/client.pem --key devices/offline-device/pkey.pem <repo_server>/targets.json | jq
```
```
curl --cacert factory-root/factory-root-ca.crt --cert devices/offline-device/client.pem --key devices/offline-device/pkey.pem <ostree_server>/targets.json | jq
```
