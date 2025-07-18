[comment]: <> (Code generated by mdatagen. DO NOT EDIT.)

# nginx

## Default Metrics

The following metrics are emitted by default. Each of them can be disabled by applying the following configuration:

```yaml
metrics:
  <metric_name>:
    enabled: false
```

### nginx.http.connection.count

The current number of connections.

| Unit | Metric Type | Value Type |
| ---- | ----------- | ---------- |
| connections | Gauge | Int |

#### Attributes

| Name | Description | Values |
| ---- | ----------- | ------ |
| nginx.connections.outcome | The outcome of the connection. | Str: ``ACCEPTED``, ``ACTIVE``, ``HANDLED``, ``READING``, ``WRITING``, ``WAITING`` |

### nginx.http.connections

The total number of connections, since NGINX was last started or reloaded.

| Unit | Metric Type | Value Type | Aggregation Temporality | Monotonic |
| ---- | ----------- | ---------- | ----------------------- | --------- |
| connections | Sum | Int | Cumulative | true |

#### Attributes

| Name | Description | Values |
| ---- | ----------- | ------ |
| nginx.connections.outcome | The outcome of the connection. | Str: ``ACCEPTED``, ``ACTIVE``, ``HANDLED``, ``READING``, ``WRITING``, ``WAITING`` |

### nginx.http.request.count

The total number of client requests received, since the last collection interval.

| Unit | Metric Type | Value Type |
| ---- | ----------- | ---------- |
| requests | Gauge | Int |

### nginx.http.requests

The total number of client requests received, since NGINX was last started or reloaded.

| Unit | Metric Type | Value Type | Aggregation Temporality | Monotonic |
| ---- | ----------- | ---------- | ----------------------- | --------- |
| requests | Sum | Int | Cumulative | true |

### nginx.http.response.count

The total number of HTTP responses since the last collection interval, grouped by status code range.

| Unit | Metric Type | Value Type |
| ---- | ----------- | ---------- |
| responses | Gauge | Int |

#### Attributes

| Name | Description | Values |
| ---- | ----------- | ------ |
| nginx.status_range | A status code range or bucket for a HTTP response's status code. | Str: ``1xx``, ``2xx``, ``3xx``, ``4xx``, ``5xx`` |

## Resource Attributes

| Name | Description | Values | Enabled |
| ---- | ----------- | ------ | ------- |
| instance.id | The nginx instance id. | Any Str | true |
| instance.type | The nginx instance type (nginx, nginxplus). | Any Str | true |
