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
      "name": "dear_baby_reset_onboarding",
      "description": "Reset dear-baby onboarding for the user with the given email by exec'ing the bundled /reset-onboarding CLI inside a running dear-baby backend pod. Clears onboarded_at, due_date, voice coachmark dismissal, first_record_at, and ai_preview. Records themselves are preserved.",
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
        "title": "Reset dear-baby Onboarding",
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
      "name": "grafana_cloud_token",
      "description": "Mint a short-lived Grafana Cloud access token (valid ~1 hour) from a pre-created, read-only access policy (metrics:read, logs:read) configured on the server. Takes no arguments: the access policy, region, and management credentials are fixed on the server via GRAFANA_CLOUD_ACCESS_POLICY_ID, GRAFANA_CLOUD_REGION, and GRAFANA_CLOUD_ACCESS_POLICY_TOKEN. Returns the token as a text/plain .env file (GRAFANA_CLOUD_TOKEN=...) with expiry and access policy as comments.",
      "inputSchema": {
        "type": "object",
        "properties": {},
        "additionalProperties": false
      },
      "annotations": {
        "title": "Grafana Cloud Token",
        "readOnlyHint": false,
        "destructiveHint": false,
        "idempotentHint": false,
        "openWorldHint": true
      }
    }
  ]
}`
