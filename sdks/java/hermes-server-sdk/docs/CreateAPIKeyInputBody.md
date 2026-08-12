

# CreateAPIKeyInputBody


## Properties

| Name | Type | Description | Notes |
|------------ | ------------- | ------------- | -------------|
|**$schema** | **URI** | A URL to the JSON Schema for this object. |  [optional] [readonly] |
|**name** | **String** | Human-readable key name |  |
|**permissions** | **List&lt;String&gt;** | Permission set (defaults to all except apikeys:manage) |  [optional] |
|**rateLimitBurst** | **Long** | Requests admitted instantaneously for this key. Omit to use the service default. |  [optional] |
|**rateLimitPerSecond** | **Long** | Sustained requests per second for this key. Omit to use the service default. |  [optional] |



