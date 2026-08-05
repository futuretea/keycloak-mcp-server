# Keycloak 26.6.4 Admin REST API inventory

`keycloak-admin-26.6.4-openapi.json` is the version-pinned OpenAPI definition
used to generate the MCP catalog. `keycloak-admin-26.6.4-inventory.tsv` is a
mechanical operation inventory with these tab-separated columns:

```text
path    method    operation_id    primary_tag
```

Source URL:

```text
https://www.keycloak.org/docs-api/26.6.4/rest-api/openapi.json
```

Source checkout evidence: `third-party-projects/keycloak` is at tag `26.6.4`
and commit `dc1bfc54bf1462f7e79822adb4c59aba7e25d50f`.

SHA-256 of `keycloak-admin-26.6.4-openapi.json`:

```text
c7580b4a4a732d38e91b816a4759926c64c2167d6d65ea7ffdb96eb5d0e766aa
```

The inventory contains 401 operations across 265 paths. Regenerate it with:

```bash
jq -r '
  def operation_entries: .paths | to_entries[] | .key as $path | .value | to_entries[] | .key as $method | select(["get", "post", "put", "delete", "patch", "head", "options", "trace"] | index($method)) | [$path, $method, (.value.operationId // ""), (.value.tags[0] // "untagged")];
  operation_entries | @tsv
' keycloak-admin-26.6.4-openapi.json > keycloak-admin-26.6.4-inventory.tsv
```

The raw OpenAPI definition is authoritative. The TSV is a convenience index;
when they disagree, regenerate the TSV and use the OpenAPI definition.
