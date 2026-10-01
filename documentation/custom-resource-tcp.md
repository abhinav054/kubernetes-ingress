# ![HAProxy](../assets/images/haproxy-weblogo-210x49.png "HAProxy")


# TCP Custom Resources

Refer to [Custom Resources](../custom-resources.md) for a general overview of Custom Resources for the Ingress Controller.

This documentation will focus on the `TCP` CR (Custom Resource) that allows more flexibility to configure a TCP service (Frontend and backend) that the previous way of doing it (a specialized TCP services Configmap).

It basically allows to configure any option available in `client-native` *frontend*, *bind* and *backend* sections.

**IMPORTANT NOTE**: The TCP ConfigMap and the Custom Resources `TCP` are not compatible.
If you use both (a TCP CR and the TCP confimap with a TCP Service on the same Address/Port), this would be lead to *random configuration*.
Please ensure when deploying `TCP` Custom Resources that no TCP configmap is present that contain a TCP service on the same Address/Port


## Difference of concept between Global/Default/Backend CRs and TCP CRs

- For Global/Default CRs:
The resouce have to be referenced via the `cr-global` annotation in the `Ingress Controller ConfigMap`.
- For Backend CRs: The resource has to be referenced via the `cr-backend` annotation in corresponding `backend service`. `cr-backend` annotation can be used also at the ConfigMap level (as default backend config for all services) or Ingress level (as a default backend config for the underlying services)

For the TCP CRs, no need to reference them.
TCP CRs are namespaced, you can deploy several of them in a namespace. They will all be applied.

## TCP CRD (Custom Resource Defintion)

The definition can be found [definitions](../crs/definition/)

Current implementation relies on the client-native library and its models to configure HAProxy.

```yaml
apiVersion: ingress.v3.haproxy.org/v3
kind: TCP
metadata:
  annotations:
    ingress.class: haproxy
  name: tcp-1
  namespace: test
spec:
- frontend:
    binds:
      v4:
        name: v4
        port: 32766
      v4v6:
        address: '::'
        name: v4v6
        port: 32766
        v4v6: true
    log_format: '%{+Q}o %t %s'
    name: fe-http-echo-8443
    tcplog: true
  name: tcp-http-echo-8443
  service:
    name: http-echo
    port: 8443

```

A `TCP` CR contains a list of TCP services definitions.
Each of them has:
- a `name`
- an optional `do_not_create` flag that creates only the backend and leaves frontend management to another resource or process
- a `frontend` section that contains:
  - a `frontend`: any setting from client-native frontend model is allowed (**except the `mode` that is forced to `tcp`**)
  - a list of `binds`: any setting from client-native `models.Bind` model is allowed
  - `acl_list`
  - `backend_switching_rule_list`
  - `capture_list`
  - `filter_list`
  - `log_target_list`
  - `tcp_request_rule_list`
- a `service` defintion that is an Kubernetes upstream Service/Port (the K8s Service has to be in the same namespace as the TCP CR is deployed)
- a `services`: a list of additional Kubernetes upstream Service/Port that will generated addition backends in the haproxy configuration (use `backend_switching_rule_list` to add switching rules between those backends)

The following table explains what is configurable is the Frontend section, what model it is from client-native and what keywords it generates in the haproxy configuration.

| Frontend section | client-native model | haproxy keyword |
|------------------|---------------------|-----------------|
| acl_list | `models.Acls`   | `acl`|
| backend_switching_rule_list| `models.AcBackendSwitchingRules`   | `use_backend` |
| capture_list| `models.Captures` | `declare capture` |
| filter_list | `models.Filters` | `filter` |
| log_target_list | `models.LogTargets` | `log` |
| tcp_request_rule_list | `models.TCPRequestRules` | `tcp-request` |


### Using an existing frontend with `do_not_create`

Set `do_not_create: true` when the TCP CR should create and reconcile the
backend for a Kubernetes Service without creating or modifying an HAProxy
frontend. This is useful when the frontend is managed separately, for example
through a `Frontend` CR or an external HAProxy configuration process.

The `frontend`, `name`, and `service` fields remain required by the TCP CRD.
However, when `do_not_create` is enabled, the controller ignores the frontend
configuration, including its name, binds, ACLs, and switching rules. It also
does not set the existing frontend's `default_backend`; the separately managed
frontend must reference the generated backend explicitly.

For example, this resource creates the backend
`test_svc_http-echo_https` and does not create a
`tcpcr_test_externally-managed` frontend:

```yaml
apiVersion: ingress.v3.haproxy.org/v3
kind: TCP
metadata:
  annotations:
    ingress.class: haproxy
  name: tcp-backend
  namespace: test
spec:
- name: http-echo-backend
  do_not_create: true
  frontend:
    name: externally-managed
  service:
    name: http-echo
    port: 8443
```

The separately managed HAProxy frontend can then route to that backend, for
example with `default_backend test_svc_http-echo_https`.

Entries with `do_not_create: true` do not participate in TCP CR frontend-name
or bind-address collision detection because they do not own a frontend or any
binds. Deleting the TCP CR removes its generated backend, but does not remove or
change the separately managed frontend.


### Full example and corresponding haproxy configuration

For a full example with all sections, please refere to [Full example)](./tcp-cr-full-example)

In this folder, you will find a complete example:
- [echo](./tcp-cr-full-example/echo.yaml): an application deployment + service that will be the default backend
- [echo-0](./tcp-cr-full-example/echo-0.yaml)/[echo-1](./tcp-cr-full-example/echo-1.yaml): 2 application deployment + services that will be used for backend switching rules with acl
- the corresponding [generated haproxy configuration](./tcp-cr-full-example/haproxy.cfg)

In the example, the resources have been deployed in a namespace `tests`.


Note that in the TCP CR :
- `.spec.service` will create:
  - the backend `backend tests_svc_http-echo_https`
  - the frontend `default_backend`
- `.spec.services` will create 2 additional backends:
  - `tests_svc_http-echo-0_https`
  - `tests_svc_http-echo-1_https`
- The `acl` and `use_backend` are handled by the TCP CR `.spec.acl_list` and `.spec.backend_switching_rule_list`.

Except the frontend keyword `default_backend`, all other lines are not automatically generated but are in a flexible way handled by the `frontend` section in the TCP CR.

## ingress.class

Starting `3.1`, the TCP Custom Resource managed by the Ingress Controller can be filtered using the `ingress.class` annotation.
It behaves the same way as `Ingress`:

| ingress.class controller flag | TCP CR ingress.class annotation | Behavior |
|------------------|---------------------|-----------------|
| '' (not set) |  * (any value) | TCP CR managed by IC |
| \<igclass\> | Same value as controller  | TCP CR managed by IC |
| \<igclass\> | Value different from controller  | TCP CR not managed by IC, frontend and backend deleted if existing |
| \<igclass\> | '' (empty, not set)| if controller `empty-ingress-class` flag is set, TCP CR managed by IC, otherwise ignored (and frontend and backend are deleted)|


### Migration 3.0 to 3.1: action required regarding ingress.class annotation

If some TCP CRs were deployed with Ingress Controller version <= v3.0, and the Ingress Controller has a `ingress.class` flag for the controller, the TCP CRs need to have the same value for the `ingress.class` annotation in the TCP CR.

If the annotation is not set, the corresponding backends and frontends in the haproxy configuration would be deleted:
- except if the controller `empty-ingress-class` flag is set (same behavior as for `Ingress`).

The setting of the `ingress.class` to the TCP CRs should be done **prior to the upgrade to** `v3.1`. It will not be used in v3.0 but needs to be there starting v3.1.



## Pod and Service definitions

with the following Kubernetes Service and Pod manifests:


```yaml
---
kind: Deployment
apiVersion: apps/v1
metadata:
  name: http-echo
  namespace: test
spec:
  replicas: 1
  selector:
    matchLabels:
      app: http-echo
  template:
    metadata:
      creationTimestamp: null
      labels:
        app: http-echo
    spec:
      containers:
        - name: http-echo
          image: haproxytech/http-echo:latest
          imagePullPolicy: Never
          args:
          - --default-response=hostname
          ports:
            - name: http
              containerPort: 8888
              protocol: TCP
            - name: https
              containerPort: 8443
              protocol: TCP
---
kind: Service
apiVersion: v1
metadata:
  name: http-echo
  namespace: test
spec:
  ipFamilyPolicy: RequireDualStack
  ports:
    - name: http
      protocol: TCP
      port: 8888
      targetPort: http
    - name: https
      protocol: TCP
      port: 8443
      targetPort: https
  selector:
    app: http-echo
---

```


### HAProxy configuration generated for this TCP CR

#### Frontend sections


```
frontend tcpcr_test_fe-http-echo-8443
  mode tcp
  bind :32766 name v4
  bind [::]:32766 name v4v6 v4v6
  log-format '%{+Q}o %t %s'
  option tcplog
  default_backend test_svc_http-echo_https
```

The frontend name `tcpcr_test_fe-http-echo-443` follow the pattern:
- tcpcr_\<namespace\>_\<tcpcr.frontend.name\>

#### Backend sections

Server names are derived from the endpoint address and port; there are no spare disabled slots.

```
backend test_svc_http-echo_https
  mode tcp
  balance roundrobin
  no option abortonclose
  timeout server 50000
  default-server check
  server s8a3c0d9e2f14b6a7c5d1e0f [fd00:10:244::8]:8443 enabled
  server s1f2e3d4c5b6a79880716253 10.244.0.8:8443 enabled
```


## How to configure the backend ?

You can use the `Backend CR` (and reference it in the Ingress Controller Configmap or the Ingress or the Service) in conjonction to the TCP CR.

For example, by adding the following Backend CR in the `test` namespace:

<details>
<summary>Backend CR</summary>

```yaml
apiVersion: ingress.v3.haproxy.org/v3
kind: Backend
metadata:
  name: mybackend
  namespace: haproxy-controller
spec:
  abortonclose: disabled
  balance:
    algorithm: leastconn
  default_server:
    check-sni: example.com
    resolve-prefer: ipv4
    sni: str(example.com)
    verify: none
  mode: http
  name: toto

```
</details>


The following backend section would be generated in the HAProxy configuration instead of what was explained above:

```
backend test_svc_http-echo_https
  mode tcp
  balance leastconn
  no option abortonclose
  default-server check-sni example.com resolve-prefer ipv4 sni str(example.com) verify none
  server s8a3c0d9e2f14b6a7c5d1e0f 10.244.0.64:8443 enabled

```

## Collisions

2 types of collisions are detected and managed:
- collisions on frontend names
- collisions on bind address/port

In case several TCPs (*accross all namespaces*) have this kind of collisions, we only apply the one that was created first based on the older CreationTimestamp of the CR.

For example, with using the previous `http-echo` deployement and service, and the already deplyed TCP `tcp-1` in namespace `test`, if we try to deploy the following TCP (that has a collision on Address/Port with the existing TCP `tcp-1`):
```yaml
apiVersion: ingress.v3.haproxy.org/v3
kind: TCP
metadata:
  annotations:
    ingress.class: haproxy
  name: tcp-2
  namespace: test
spec:
- frontend:
    binds:
      v4:
        name: v4
        port: 32766
    log_format: '%{+Q}o'
    name: fe-http-echo-test2-8443
    tcplog: true
  name: tcp-http-echo-test2-8443
  service:
    name: http-echo
    port: 8443
```


There will also be an ERROR log
```
 2024/06/19 13:47:05 ERROR   handler/tcp-cr.go:61 [transactionID=dab63ebf-238d-4e04-b844-af668a86b024] tcp-cr: skipping tcp 'test/tcp-2/tcp-http-echo-test2-8443' due to collision - Colli │
│ sion AddPort :32766 with test/tcp-1/tcp-http-echo-8443
```

explaining that :
- the TCP (in namespace `test`) named `tcp2` that has a tcp service specification named `tcp-http-echo-test2-8443`
 will not be applied
- in favor of the oldest one (in namespace `test`) named `tcp1` with a tcp service specification named `tcp-http-echo-8443`
- due a collision on AddressPort (`AddPort`)

*This works accross all namespaces*

## TLS on a TCP frontend

To enable TLS on a frontend created by a TCP CR, set `ssl: true` on the bind
and omit `ssl_certificate`. The controller binds the shared frontend
certificate directory to the TCP frontend.

Use a `TLS` resource to add a Kubernetes TLS Secret to that directory. The TLS
resource must reference the generated HAProxy frontend name, which follows the
`tcpcr_<namespace>_<tcpcr.frontend.name>` pattern.

~~~yaml
apiVersion: ingress.v3.haproxy.org/v3
kind: TCP
metadata:
  annotations:
    ingress.class: haproxy
  name: tcp-1
  namespace: test
spec:
- frontend:
    binds:
      v4:
        name: v4
        port: 32766
        ssl: true
    log_format: '%{+Q}o %t %s'
    name: fe-http-echo-443
    tcplog: true
  name: tcp-http-echo-443
  service:
    name: http-echo
    port: 443
---
apiVersion: ingress.v3.haproxy.org/v3
kind: TLS
metadata:
  name: tcp-test-tls
  namespace: test
spec:
  frontend: tcpcr_test_fe-http-echo-443
  secretName: tcp-test-cert
~~~

The `tcp-test-cert` Secret must be in the same namespace as the `TLS`
resource and contain `tls.crt` and `tls.key`. The TLS resource stores the
certificate in the shared frontend certificate directory. In cluster mode, the
generated bind uses:

~~~text
bind :32766 name v4 crt /etc/haproxy/certs/frontend ssl
~~~

In external mode, the directory is `<config-dir>/certs/frontend`, where
`<config-dir>` is `/tmp/haproxy-ingress/etc` by default or the value of the
controller's `--config-dir` argument.

The TLS resource also enables SSL on every bind of the referenced frontend. The
explicit `ssl: true` in the TCP CR makes the TCP listener's intent clear and
ensures it uses the frontend certificate directory even before the TLS resource
is reconciled.
