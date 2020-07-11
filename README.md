# butterbot

Passes butter by requesting configured external services, keeping track of responses, 
and notifying of state changes.

## config.yaml

Drop a `config.yaml` into the same directory as the binary that looks like this:

```yaml
butterbot:
  notifiers:
    - name: discord
      url: "https://discordapp.com/api/webhooks/some/webhook"
      content_type: "application/json"
      status_code: 204
    #name: ...
  checks:
    - name: some_app
      parameters:
        method: "http"
        verb: "get"
        url: "https://app.com/endpoint/"
        code: 200
        skip_verify: false
        timeout: 5
      status: false  # desired initial state; if true then first status notification is up 
      message: "initial state"
    #- name: ...

```
