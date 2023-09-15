# Need the followings

- `params.go`
- `component.go`
  - Create the connection to the Elasticsearch server
    - The connection should be to the port of logstash (not kibana or elasticsearch)
  - Make it accessible for this component
    - Example

```go
	deps.Protocol.Events.Engine.Notarization.SlotCommitted.Hook(func(scd *notarization.SlotCommittedDetails) {
		// Send the log to the Elasticsearch server
	})

```

- The sent log/structure can be similar to the ones in GoShimmer

  - Refer to GoShimmer to `plugins/remotemetrics/block.go`
  - In the style of components

- Kibana (the frontend of elastic search)
  - [Videos](https://www.elastic.co/videos/training-how-to-series-stack?elektra=kibana-dashboard&storm=hero)
- Check the hooked events
