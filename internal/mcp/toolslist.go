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
      "name": "api_resources",
      "description": "List the resource kinds this cluster actually serves (group, version, kind, plural name, namespaced). Use it to resolve a kind before calling resource_list or resource_get.",
      "inputSchema": {
        "type": "object",
        "properties": {},
        "additionalProperties": false
      },
      "annotations": {
        "title": "API Resources",
        "readOnlyHint": true,
        "idempotentHint": true,
        "openWorldHint": false
      }
    },
    {
      "name": "resource_list",
      "description": "List any Kubernetes resource by coordinate (apiVersion + kind), returned as the apiserver's table rendering. Namespace is optional; omit it to list across all namespaces. Exercises the list verb only. A kind outside this server's RBAC grant is reported as such rather than retried.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "apiVersion": {
            "type": "string",
            "description": "Group/version of the kind, e.g. \"v1\" or \"apps/v1\"."
          },
          "kind": {
            "type": "string",
            "description": "Kind to list, e.g. \"Pod\", \"Deployment\", \"Ingress\"."
          },
          "namespace": {
            "type": "string",
            "description": "Namespace. Optional; omitted = all namespaces. Rejected for cluster-scoped kinds."
          },
          "labelSelector": {
            "type": "string",
            "description": "Label selector, applied server-side."
          },
          "fieldSelector": {
            "type": "string",
            "description": "Field selector, applied server-side."
          },
          "limit": {
            "type": "integer",
            "minimum": 1,
            "maximum": 500,
            "description": "Page size. Defaults to 100; values above 500 are rejected, not clamped."
          },
          "continue": {
            "type": "string",
            "description": "Continue token from a previous truncated page."
          }
        },
        "required": ["apiVersion", "kind"],
        "additionalProperties": false
      },
      "annotations": {
        "title": "List Resources",
        "readOnlyHint": true,
        "idempotentHint": true,
        "openWorldHint": false
      }
    },
    {
      "name": "resource_watch",
      "description": "Observe change events (ADDED/MODIFIED/DELETED) for a coordinate over one bounded window, then return them. Exercises the watch verb only. The stream is not held open across calls: the window closes and the collected events come back, so this is how you wait for a rollout or for a pod to go Ready without polling resource_list. Pass resourceVersion to resume after a previous window. Sensitive kinds (Secret) require human approval, because a stream hands over the whole object exactly as a read does.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "apiVersion": {
            "type": "string",
            "description": "Group/version of the kind, e.g. \"v1\" or \"apps/v1\"."
          },
          "kind": {
            "type": "string",
            "description": "Kind to watch, e.g. \"Pod\", \"Deployment\", \"Event\"."
          },
          "namespace": {
            "type": "string",
            "description": "Namespace. Optional; omitted = all namespaces. Rejected for cluster-scoped kinds."
          },
          "labelSelector": {
            "type": "string",
            "description": "Label selector, applied server-side."
          },
          "fieldSelector": {
            "type": "string",
            "description": "Field selector, applied server-side."
          },
          "watchSeconds": {
            "type": "integer",
            "minimum": 1,
            "maximum": 60,
            "description": "Length of the observation window. Defaults to 10; values above 60 are rejected, not clamped."
          },
          "resourceVersion": {
            "type": "string",
            "description": "Receive only changes after this version. Take it from metadata.resourceVersion of the last event of a previous window."
          }
        },
        "required": ["apiVersion", "kind"],
        "additionalProperties": false
      },
      "annotations": {
        "title": "Watch Resources",
        "readOnlyHint": true,
        "idempotentHint": false,
        "openWorldHint": false
      }
    },
    {
      "name": "resource_get",
      "description": "Read one Kubernetes object whole by coordinate and name, or one of its log/scale/status subresources. Exercises the get verb only; name is required, so find the object with resource_list first. Sensitive kinds (Secret) require human approval, and a kind outside this server's RBAC grant is reported as such.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "apiVersion": {
            "type": "string",
            "description": "Group/version of the kind, e.g. \"v1\" or \"apps/v1\"."
          },
          "kind": {
            "type": "string",
            "description": "Kind to read, e.g. \"Pod\", \"Deployment\", \"ConfigMap\"."
          },
          "namespace": {
            "type": "string",
            "description": "Namespace. Required for namespaced kinds, rejected for cluster-scoped ones."
          },
          "name": {
            "type": "string",
            "description": "Object name."
          },
          "subresource": {
            "type": "string",
            "enum": ["log", "scale", "status"],
            "description": "Subresource to read instead of the object itself."
          },
          "container": {
            "type": "string",
            "description": "subresource=log only. Required when the pod has more than one container."
          },
          "tailLines": {
            "type": "integer",
            "minimum": 1,
            "maximum": 5000,
            "description": "subresource=log only. Lines from the end of the log. Default 200."
          },
          "previous": {
            "type": "boolean",
            "description": "subresource=log only. Read the previous terminated container instance."
          },
          "timestamps": {
            "type": "boolean",
            "description": "subresource=log only. Prefix each line with an RFC3339 timestamp."
          },
          "sinceSeconds": {
            "type": "integer",
            "minimum": 1,
            "description": "subresource=log only. Only return logs newer than this many seconds."
          }
        },
        "required": ["apiVersion", "kind", "name"],
        "additionalProperties": false
      },
      "annotations": {
        "title": "Get Resource",
        "readOnlyHint": true,
        "idempotentHint": true,
        "openWorldHint": false
      }
    },
    {
      "name": "resource_create",
      "description": "Create named Kubernetes objects without overwriting existing objects. Accepts one manifest object or a YAML/JSON string with one or more documents separated by ---. Each document must contain apiVersion, kind and metadata.name; namespaced objects also need metadata.namespace. Obtains one approval per document, all before the first create. Rejection creates nothing. Stops at the first execution error without rollback or retry and reports created, failed and unattempted coordinates. Existing names fail with 409. Sensitive values are masked in approval context.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "manifest": {
            "oneOf": [{"type": "object"}, {"type": "string", "minLength": 1}],
            "description": "One object or a YAML/JSON document stream. Coordinates come from each document; namespace is never defaulted."
          }
        },
        "required": ["manifest"],
        "additionalProperties": false
      },
      "annotations": {
        "title": "Create Resource",
        "readOnlyHint": false,
        "destructiveHint": false,
        "idempotentHint": false,
        "openWorldHint": false
      }
    },
    {
      "name": "resource_update",
      "description": "Replace one Kubernetes object whole (PUT) by coordinate and name, or set a replica count through subresource=scale. Exercises the update verb only and always requires human approval. Without subresource it takes a manifest; with subresource=scale it takes replicas (0 is allowed, negative and missing are rejected, and a kind with no scale subresource such as DaemonSet is refused for having no replicas). Rolling restarts and partial edits are resource_patch, not this tool.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "apiVersion": {
            "type": "string",
            "description": "Group/version of the kind, e.g. \"v1\" or \"apps/v1\"."
          },
          "kind": {
            "type": "string",
            "description": "Kind to replace, e.g. \"Deployment\", \"ConfigMap\"."
          },
          "namespace": {
            "type": "string",
            "description": "Namespace. Required for namespaced kinds, rejected for cluster-scoped ones."
          },
          "name": {
            "type": "string",
            "description": "Object name. Required; this tool replaces one object, not a selection."
          },
          "subresource": {
            "type": "string",
            "enum": ["scale"],
            "description": "Omit to replace the whole object. scale writes a replica count instead."
          },
          "manifest": {
            "type": "object",
            "description": "The replacement object, sent as a PUT. Whole-object replacement only; rejected with subresource=scale. Its apiVersion, kind and metadata.name must agree with the coordinate."
          },
          "replicas": {
            "type": "integer",
            "minimum": 0,
            "description": "subresource=scale only, where it is required. 0 is allowed; negative is rejected."
          }
        },
        "required": ["apiVersion", "kind", "name"],
        "additionalProperties": false
      },
      "annotations": {
        "title": "Update Resource",
        "readOnlyHint": false,
        "destructiveHint": true,
        "idempotentHint": true,
        "openWorldHint": false
      }
    },
    {
      "name": "resource_patch",
      "description": "Apply a patch to one Kubernetes object by coordinate and name. Exercises the patch verb only and always requires human approval. patchType selects merge, strategic, json (RFC 6902) or apply (server-side apply, which also requires fieldManager). A rolling restart is this tool with a strategic patch that sets the kubectl.kubernetes.io/restartedAt pod-template annotation.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "apiVersion": {
            "type": "string",
            "description": "Group/version of the kind, e.g. \"v1\" or \"apps/v1\"."
          },
          "kind": {
            "type": "string",
            "description": "Kind to patch, e.g. \"Deployment\", \"ConfigMap\"."
          },
          "namespace": {
            "type": "string",
            "description": "Namespace. Required for namespaced kinds, rejected for cluster-scoped ones."
          },
          "name": {
            "type": "string",
            "description": "Object name. Required; this tool patches one object, not a selection."
          },
          "patchType": {
            "type": "string",
            "enum": ["merge", "strategic", "json", "apply"],
            "description": "How the body is interpreted. json is an RFC 6902 operation array; the other three are objects."
          },
          "patch": {
            "type": ["object", "array"],
            "description": "The patch body, sent to the apiserver unchanged. An RFC 6902 array when patchType=json, otherwise a partial object."
          },
          "fieldManager": {
            "type": "string",
            "description": "patchType=apply only, where it is required. Server-side apply records it as the owner of the fields this patch sets."
          }
        },
        "required": ["apiVersion", "kind", "name", "patchType", "patch"],
        "additionalProperties": false
      },
      "annotations": {
        "title": "Patch Resource",
        "readOnlyHint": false,
        "destructiveHint": true,
        "idempotentHint": false,
        "openWorldHint": false
      }
    },
    {
      "name": "resource_delete",
      "description": "Delete one Kubernetes object by coordinate and name. Exercises the delete verb only and always requires human approval. name is required and there is no selector argument: deleting a selection is the deletecollection verb, which resource_delete_collection holds. gracePeriodSeconds overrides the kind's own termination grace period, and 0 means do not wait. The call is answered when the apiserver accepts it, which is before finalizers and the grace period have run.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "apiVersion": {
            "type": "string",
            "description": "Group/version of the kind, e.g. \"v1\" or \"apps/v1\"."
          },
          "kind": {
            "type": "string",
            "description": "Kind to delete, e.g. \"Pod\", \"ConfigMap\"."
          },
          "namespace": {
            "type": "string",
            "description": "Namespace. Required for namespaced kinds, rejected for cluster-scoped ones."
          },
          "name": {
            "type": "string",
            "description": "Object name. Required; this tool removes one object, not a selection."
          },
          "gracePeriodSeconds": {
            "type": "integer",
            "minimum": 0,
            "description": "Seconds to wait before the object is removed, overriding the kind's default. 0 removes it without waiting."
          }
        },
        "required": ["apiVersion", "kind", "name"],
        "additionalProperties": false
      },
      "annotations": {
        "title": "Delete Resource",
        "readOnlyHint": false,
        "destructiveHint": true,
        "idempotentHint": true,
        "openWorldHint": false
      }
    },
    {
      "name": "resource_exec",
      "description": "Run one command inside a container of one named Kubernetes pod (the pods/exec subresource) and return stdout and stderr separately. Exercises the create verb on pods/exec only and always requires human approval. container is required when the pod has more than one container; the apiserver's refusal names the candidates. Output is capped at 256KiB per stream and the run at 30 seconds — a response cut by a cap says so instead of ending early.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "apiVersion": {
            "type": "string",
            "description": "Group/version of the pod kind, e.g. \"v1\"."
          },
          "kind": {
            "type": "string",
            "description": "Kind to exec in, e.g. \"Pod\". A kind with no exec subresource is refused."
          },
          "namespace": {
            "type": "string",
            "description": "Namespace. Required for pods; rejected for cluster-scoped kinds."
          },
          "name": {
            "type": "string",
            "description": "Pod name. Required; this tool runs one command in one named pod, not a selection."
          },
          "container": {
            "type": "string",
            "description": "Container to run in. Omit only when the pod has one container; the refusal for an omitted name lists the candidates."
          },
          "command": {
            "type": "array",
            "items": { "type": "string" },
            "minItems": 1,
            "description": "Array of argv to run, e.g. [\"echo\", \"hi\"]. The approval screen carries it verbatim."
          }
        },
        "required": ["apiVersion", "kind", "name", "command"],
        "additionalProperties": false
      },
      "annotations": {
        "title": "Exec Resource",
        "readOnlyHint": false,
        "destructiveHint": true,
        "idempotentHint": false,
        "openWorldHint": false
      }
    },
    {
      "name": "resource_attach",
      "description": "Attach to the process already running in a container of one named Kubernetes pod (the pods/attach subresource) and return what its streams say during a short read window. Nothing is started: this joins the existing main process rather than running a command, so there is no exit code. Exercises the create verb on pods/attach only and always requires human approval — the power is exec's, because attaching stdin to a pod whose PID 1 is a shell is indistinguishable from opening one. readSeconds defaults to 5 and may not exceed 30; stdin, when given, is written once right after attaching. Output is capped at 256KiB per stream and a cut stream says so.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "apiVersion": {
            "type": "string",
            "description": "Group/version of the pod kind, e.g. \"v1\"."
          },
          "kind": {
            "type": "string",
            "description": "Kind to attach to, e.g. \"Pod\". A kind with no attach subresource is refused."
          },
          "namespace": {
            "type": "string",
            "description": "Namespace. Required for pods; rejected for cluster-scoped kinds."
          },
          "name": {
            "type": "string",
            "description": "Pod name. Required; this tool attaches to one named pod, not a selection."
          },
          "container": {
            "type": "string",
            "description": "Container to attach to. Omit only when the pod has one container; the refusal for an omitted name lists the candidates."
          },
          "stdin": {
            "type": "string",
            "description": "Payload written to the attached process once, right after attaching. The approval screen carries it verbatim. Omit to attach read-only."
          },
          "readSeconds": {
            "type": "integer",
            "minimum": 1,
            "maximum": 30,
            "description": "How long to collect output before answering. Defaults to 5. The process keeps running after the window closes."
          }
        },
        "required": ["apiVersion", "kind", "name"],
        "additionalProperties": false
      },
      "annotations": {
        "title": "Attach Resource",
        "readOnlyHint": false,
        "destructiveHint": true,
        "idempotentHint": false,
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
      "description": "Full-text search over the preconfigured OpenSearch collection. Provide 'query' (the search text); optionally scope the search to a single index with 'index' (omitted = every index in the collection) and control the result count with 'size' (default 10, maximum 50 — larger values are rejected, not clamped). Returns matching documents with their index, id, relevance score, and body (_source).",
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
      "description": "Index (upsert) a JSON document into an index of the preconfigured OpenSearch collection. Provide 'index' and 'document' (a JSON object); optionally provide 'id' to upsert that exact document — re-putting an existing id overwrites it (result 'updated'), while omitting 'id' auto-generates one (result 'created'). The target index is created automatically on first write. The document becomes searchable after the next refresh, not instantly. Returns index, id, and result.",
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
      "description": "Delete a single document by id from an index of the preconfigured OpenSearch collection. Returns result 'deleted', or 'not_found' when the document (or index) does not exist — repeated deletes of the same id converge on 'not_found'. Only single-document deletion is exposed: no index deletion and no delete-by-query.",
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
    },
    {
      "name": "session_list",
      "description": "List the sessions the session-platform control plane holds. The control plane is reachable only from inside the cluster, so this tool is the way to see the inventory without an ingress or a port-forward. Takes no arguments. Returns each session's id, name, workloadType (shell | claude-code), state (active | idle | snapshot), pod (absent while a session is snapshotted and its pods are reclaimed), createdAt and lastAccess. Listing is passive: it does not promote an idle session, restore a snapshot, or refresh lastAccess. An empty control plane returns an empty list, not an error. Requires SESSION_PLATFORM_ENDPOINT on the server.",
      "inputSchema": {
        "type": "object",
        "properties": {},
        "additionalProperties": false
      },
      "annotations": {
        "title": "List Sessions",
        "readOnlyHint": true,
        "destructiveHint": false,
        "idempotentHint": true,
        "openWorldHint": true
      }
    },
    {
      "name": "session_read",
      "description": "Read a session-platform session's accumulated workload output from a byte-offset cursor. Pass 'offset' 0 (or omit it) for everything since the session started, then pass the returned 'nextOffset' to get only what has accumulated since; the same cursor always returns the same span, so reading never consumes output. What the output is depends on the session's workloadType: for 'shell' it is merged PTY stdout/stderr, for 'claude-code' it is the assistant text deltas plus diagnostic stderr. Reading is NOT passive: the control plane activates the target first, so an idle session is promoted and a snapshotted session is restored (its pod is recreated) before the read. The branch that served the call is reported as 'path' ('active', 'idle->active->read' or 'snapshot->restore->read') and the session is returned as it stands after the read, so that side effect is never silent. An unknown id is a not-found error and a negative offset is an argument error; neither touches the session. Requires SESSION_PLATFORM_ENDPOINT on the server.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "id": {
            "type": "string",
            "description": "Session id, as reported by session_list."
          },
          "offset": {
            "type": "integer",
            "minimum": 0,
            "description": "Server-issued byte cursor from a previous read's nextOffset. Defaults to 0, which returns the full output accumulated since session start. Use the server's cursor rather than a computed string length."
          }
        },
        "required": ["id"],
        "additionalProperties": false
      },
      "annotations": {
        "title": "Read Session Output",
        "readOnlyHint": false,
        "destructiveHint": false,
        "idempotentHint": true,
        "openWorldHint": true
      }
    },
    {
      "name": "session_write",
      "description": "Inject input into a session-platform session's workload. What the payload means depends on the session's workloadType: for 'shell' it is written to the PTY's stdin (a command, or keystrokes), for 'claude-code' it is queued as one prompt run. Either way the call is NON-BLOCKING — it returns once the control plane accepts the payload, not once the workload finishes — so the response carries no output and anything produced in response is recovered afterwards with session_read. Writing is NOT passive: like session_read it activates its target first, so an idle session is promoted and a snapshotted session is restored (its pod is recreated) rather than refused. The branch that served the call is reported as 'path' ('active', 'idle->active->write' or 'snapshot->restore->write') and the session is returned as it stands after the write, so a write that revived a parked session is never silent. Refusals arrive distinguished: an unknown id is a not-found error, a payload over the per-write limit (1 MiB per claude-code prompt) and an exhausted output quota are both permanent (retrying will not help, and existing output stays readable), while a full prompt queue is the one refusal worth retrying once a queued prompt finishes. Requires SESSION_PLATFORM_ENDPOINT on the server.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "id": {
            "type": "string",
            "description": "Session id, as reported by session_list."
          },
          "payload": {
            "type": "string",
            "description": "Raw workload input. For a 'shell' session this goes to the PTY stdin, so include a trailing newline to submit a command. For a 'claude-code' session this is one prompt, limited to 1 MiB of UTF-8 bytes."
          }
        },
        "required": ["id", "payload"],
        "additionalProperties": false
      },
      "annotations": {
        "title": "Write to Session",
        "readOnlyHint": false,
        "destructiveHint": true,
        "idempotentHint": false,
        "openWorldHint": true
      }
    }
  ]
}`
