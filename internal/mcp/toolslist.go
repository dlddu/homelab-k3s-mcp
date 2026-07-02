package mcp

// toolsListJSON is the static tools/list result. It is kept as a literal so the
// advertised JSON Schemas and annotations are easy to audit against the docs.
const toolsListJSON = `{
  "tools": [
    {
      "name": "ping",
      "description": "Health-check tool that always returns 'pong'.",
      "inputSchema": {
        "type": "object",
        "properties": {},
        "additionalProperties": false
      },
      "annotations": {
        "title": "Ping",
        "readOnlyHint": true,
        "idempotentHint": true,
        "openWorldHint": false
      }
    },
    {
      "name": "namespace_list",
      "description": "List all Kubernetes namespaces with their phase (Active, Terminating) and creation timestamp.",
      "inputSchema": {
        "type": "object",
        "properties": {},
        "additionalProperties": false
      },
      "annotations": {
        "title": "List Namespaces",
        "readOnlyHint": true,
        "idempotentHint": true,
        "openWorldHint": false
      }
    },
    {
      "name": "workload_list",
      "description": "List Kubernetes workloads (Deployment, StatefulSet, DaemonSet). Namespace is optional; omit it to list across all namespaces.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "kind": {
            "type": "string",
            "enum": ["Deployment", "StatefulSet", "DaemonSet"],
            "description": "Workload kind."
          },
          "namespace": {
            "type": "string",
            "description": "Namespace. Optional; omitted = all namespaces."
          }
        },
        "required": ["kind"],
        "additionalProperties": false
      },
      "annotations": {
        "title": "List Workloads",
        "readOnlyHint": true,
        "idempotentHint": true,
        "openWorldHint": false
      }
    },
    {
      "name": "workload_restart",
      "description": "Trigger a rolling restart of a Kubernetes workload (Deployment, StatefulSet, DaemonSet).",
      "inputSchema": {
        "type": "object",
        "properties": {
          "kind": {
            "type": "string",
            "enum": ["Deployment", "StatefulSet", "DaemonSet"],
            "description": "Workload kind."
          },
          "namespace": {
            "type": "string",
            "description": "Namespace of the workload."
          },
          "name": {
            "type": "string",
            "description": "Workload name."
          }
        },
        "required": ["kind", "namespace", "name"],
        "additionalProperties": false
      },
      "annotations": {
        "title": "Restart Workload",
        "readOnlyHint": false,
        "destructiveHint": true,
        "idempotentHint": false,
        "openWorldHint": false
      }
    },
    {
      "name": "workload_scale",
      "description": "Scale a Kubernetes workload by setting spec.replicas. Supports Deployment and StatefulSet. DaemonSets do not have replicas and are rejected.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "kind": {
            "type": "string",
            "enum": ["Deployment", "StatefulSet"],
            "description": "Workload kind."
          },
          "namespace": {
            "type": "string",
            "description": "Namespace of the workload."
          },
          "name": {
            "type": "string",
            "description": "Workload name."
          },
          "replicas": {
            "type": "integer",
            "minimum": 0,
            "description": "Desired replica count (>= 0)."
          }
        },
        "required": ["kind", "namespace", "name", "replicas"],
        "additionalProperties": false
      },
      "annotations": {
        "title": "Scale Workload",
        "readOnlyHint": false,
        "destructiveHint": true,
        "idempotentHint": true,
        "openWorldHint": false
      }
    },
    {
      "name": "workload_logs",
      "description": "Fetch container logs from a Kubernetes workload (Deployment, StatefulSet, DaemonSet). Resolves the workload's pod selector and returns logs from the first Running pod (or any matching pod when none is Running, so previous=true works after a crash loop).",
      "inputSchema": {
        "type": "object",
        "properties": {
          "kind": {
            "type": "string",
            "enum": ["Deployment", "StatefulSet", "DaemonSet"],
            "description": "Workload kind."
          },
          "namespace": {
            "type": "string",
            "description": "Namespace of the workload."
          },
          "name": {
            "type": "string",
            "description": "Workload name."
          },
          "container": {
            "type": "string",
            "description": "Container name. Required when the pod has more than one container."
          },
          "tail_lines": {
            "type": "integer",
            "minimum": 1,
            "maximum": 5000,
            "description": "Number of trailing log lines to return. Defaults to 200; capped at 5000."
          },
          "previous": {
            "type": "boolean",
            "description": "Return logs from a previously terminated container instance. Defaults to false."
          },
          "timestamps": {
            "type": "boolean",
            "description": "Prefix each log line with an RFC3339 timestamp. Defaults to false."
          },
          "since_seconds": {
            "type": "integer",
            "minimum": 1,
            "description": "Only return logs newer than this many seconds. Optional."
          }
        },
        "required": ["kind", "namespace", "name"],
        "additionalProperties": false
      },
      "annotations": {
        "title": "View Workload Logs",
        "readOnlyHint": true,
        "idempotentHint": true,
        "openWorldHint": false
      }
    },
    {
      "name": "pod_describe",
      "description": "Return a kubectl-describe-style snapshot of a single pod: metadata, container statuses (state, reason, restart count, exit code), conditions, and recent events. Events are best-effort and may be empty if the apiserver does not expose them to this service account. Provide exactly one of: 'name' (exact pod name), 'selector' (label selector; first Running pod wins), or 'workload_kind' + 'workload_name' (resolves the workload's pod selector).",
      "inputSchema": {
        "type": "object",
        "properties": {
          "namespace": {
            "type": "string",
            "description": "Namespace of the pod."
          },
          "name": {
            "type": "string",
            "description": "Exact pod name. Mutually exclusive with 'selector' and 'workload_kind'+'workload_name'."
          },
          "selector": {
            "type": "string",
            "description": "Label selector (e.g. 'app=api'). Resolves to the first Running pod matching the selector, falling back to any matching pod when none is Running."
          },
          "workload_kind": {
            "type": "string",
            "enum": ["Deployment", "StatefulSet", "DaemonSet"],
            "description": "Workload kind to resolve a pod from. Requires 'workload_name'."
          },
          "workload_name": {
            "type": "string",
            "description": "Workload name. Requires 'workload_kind'."
          }
        },
        "required": ["namespace"],
        "additionalProperties": false
      },
      "annotations": {
        "title": "Describe Pod",
        "readOnlyHint": true,
        "idempotentHint": true,
        "openWorldHint": false
      }
    },
    {
      "name": "dear_baby_reset_user",
      "description": "Reset dear-baby onboarding for the user with the given email by exec'ing the bundled /reset-user CLI inside a running dear-baby backend pod. Clears onboarded_at, due_date, voice coachmark dismissal, first_record_at, and ai_preview. Records themselves are preserved.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "namespace": {
            "type": "string",
            "description": "Namespace where the dear-baby backend is deployed."
          },
          "email": {
            "type": "string",
            "description": "Email of the user whose onboarding should be reset."
          },
          "selector": {
            "type": "string",
            "description": "Label selector for the backend pod. Defaults to 'app=dear-baby'."
          },
          "container": {
            "type": "string",
            "description": "Container name inside the pod. Defaults to 'backend'."
          }
        },
        "required": ["namespace", "email"],
        "additionalProperties": false
      },
      "annotations": {
        "title": "Reset dear-baby User",
        "readOnlyHint": false,
        "destructiveHint": true,
        "idempotentHint": true,
        "openWorldHint": false
      }
    },
    {
      "name": "github_app_installation_token",
      "description": "Mint a short-lived GitHub App installation access token (valid ~1 hour) for the installation configured on the server. Optionally scope the token to a subset of installed repositories and/or a subset of the App's permissions. Returns the token as a text/plain .env file (GITHUB_TOKEN=...) with expiry and scope as comments. Requires GITHUB_APP_CLIENT_ID, GITHUB_APP_INSTALLATION_ID, and GITHUB_APP_PRIVATE_KEY (inline PEM) on the server.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "repositories": {
            "type": "array",
            "items": { "type": "string" },
            "description": "Optional list of repository names (without owner) to restrict the token to. Each repo must be installed for the App. Omit to grant access to all installed repos."
          },
          "permissions": {
            "type": "object",
            "description": "Optional map of permission name to access level (e.g. { \"contents\": \"read\", \"pull_requests\": \"write\" }). Must be a subset of the App's installed permissions.",
            "additionalProperties": { "type": "string" }
          }
        },
        "additionalProperties": false
      },
      "annotations": {
        "title": "GitHub App Installation Token",
        "readOnlyHint": false,
        "destructiveHint": false,
        "idempotentHint": false,
        "openWorldHint": true
      }
    },
    {
      "name": "aws_config_get",
      "description": "Fetch the AWS config file from the preconfigured S3 bucket and return its contents. The bucket and key are fixed on the server via AWS_CONFIG_S3_BUCKET and AWS_CONFIG_S3_KEY, so this tool takes no arguments. The server reads the object using credentials obtained by assuming AWS_CONFIG_ROLE_ARN via STS; the base credentials for that AssumeRole call come from the default AWS credential chain (the instance profile in production). Returns the object contents as text plus metadata (size, content type, ETag, last-modified).",
      "inputSchema": {
        "type": "object",
        "properties": {},
        "additionalProperties": false
      },
      "annotations": {
        "title": "Get AWS Config File",
        "readOnlyHint": true,
        "idempotentHint": true,
        "openWorldHint": true
      }
    },
    {
      "name": "opensearch_search",
      "description": "Full-text search over the preconfigured OpenSearch Serverless collection. Provide 'query' (the search text); optionally scope the search to a single index with 'index' (omitted = every index in the collection) and control the result count with 'size' (default 10, maximum 50 — larger values are rejected, not clamped). Returns matching documents with their index, id, relevance score, and body (_source). The server signs requests with SigV4 (service 'aoss') using short-lived credentials from assuming OPENSEARCH_ROLE_ARN via STS; requires OPENSEARCH_ENDPOINT and OPENSEARCH_ROLE_ARN on the server.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "query": {
            "type": "string",
            "description": "Full-text search query."
          },
          "index": {
            "type": "string",
            "description": "Index to search. Optional; omitted = every index in the collection."
          },
          "size": {
            "type": "integer",
            "minimum": 1,
            "maximum": 50,
            "description": "Maximum number of hits to return. Defaults to 10; values above 50 are rejected."
          }
        },
        "required": ["query"],
        "additionalProperties": false
      },
      "annotations": {
        "title": "Search OpenSearch",
        "readOnlyHint": true,
        "idempotentHint": true,
        "openWorldHint": true
      }
    },
    {
      "name": "opensearch_document_put",
      "description": "Index (upsert) a JSON document into an index of the preconfigured OpenSearch Serverless collection. Provide 'index' and 'document' (a JSON object); optionally provide 'id' to upsert that exact document — re-putting an existing id overwrites it (result 'updated'), while omitting 'id' auto-generates one (result 'created'). The target index is created automatically on first write. The document becomes searchable after the next refresh, not instantly. Returns index, id, and result. Signs requests with SigV4 (service 'aoss') using short-lived AssumeRole credentials; requires OPENSEARCH_ENDPOINT and OPENSEARCH_ROLE_ARN on the server.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "index": {
            "type": "string",
            "description": "Target index. Created automatically if it does not exist."
          },
          "document": {
            "type": "object",
            "description": "The JSON document body to index."
          },
          "id": {
            "type": "string",
            "description": "Document id. Optional; when set, an existing document with the same id is overwritten (upsert). Omitted = auto-generated id."
          }
        },
        "required": ["index", "document"],
        "additionalProperties": false
      },
      "annotations": {
        "title": "Put OpenSearch Document",
        "readOnlyHint": false,
        "destructiveHint": true,
        "idempotentHint": false,
        "openWorldHint": true
      }
    },
    {
      "name": "opensearch_document_delete",
      "description": "Delete a single document by id from an index of the preconfigured OpenSearch Serverless collection. Returns result 'deleted', or 'not_found' when the document (or index) does not exist — repeated deletes of the same id converge on 'not_found'. Only single-document deletion is exposed: no index deletion and no delete-by-query. Signs requests with SigV4 (service 'aoss') using short-lived AssumeRole credentials; requires OPENSEARCH_ENDPOINT and OPENSEARCH_ROLE_ARN on the server.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "index": {
            "type": "string",
            "description": "Index containing the document."
          },
          "id": {
            "type": "string",
            "description": "Id of the document to delete."
          }
        },
        "required": ["index", "id"],
        "additionalProperties": false
      },
      "annotations": {
        "title": "Delete OpenSearch Document",
        "readOnlyHint": false,
        "destructiveHint": true,
        "idempotentHint": true,
        "openWorldHint": true
      }
    },
    {
      "name": "grafana_token",
      "description": "Mint a short-lived Grafana Cloud token (valid 1 hour) scoped to metrics and log read access, and return it with the static query endpoints and instance IDs needed to use it. The Grafana Cloud metrics (Mimir/Prometheus) and logs (Loki) endpoints use HTTP Basic auth where the password is this token and the username is the data source's numeric instance ID, so the token alone is not enough. The access policy and one-hour TTL are fixed on the server, so this tool takes no arguments. Returns a text/plain .env file with GRAFANA_METRICS_URL, GRAFANA_METRICS_USER, GRAFANA_LOGS_URL, GRAFANA_LOGS_USER and GRAFANA_TOKEN (the shared password). Requires GRAFANA_ISSUER_TOKEN, GRAFANA_READ_POLICY_ID, GRAFANA_REGION, GRAFANA_METRICS_URL, GRAFANA_METRICS_USER, GRAFANA_LOGS_URL and GRAFANA_LOGS_USER on the server.",
      "inputSchema": {
        "type": "object",
        "properties": {},
        "additionalProperties": false
      },
      "annotations": {
        "title": "Grafana Cloud Read Token",
        "readOnlyHint": false,
        "destructiveHint": false,
        "idempotentHint": false,
        "openWorldHint": true
      }
    }
  ]
}`
