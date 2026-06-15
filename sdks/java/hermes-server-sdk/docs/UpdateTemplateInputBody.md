

# UpdateTemplateInputBody


## Properties

| Name | Type | Description | Notes |
|------------ | ------------- | ------------- | -------------|
|**$schema** | **URI** | A URL to the JSON Schema for this object. |  [optional] [readonly] |
|**content** | **Map&lt;String, Map&lt;String, String&gt;&gt;** | Per-channel content: channel slug -&gt; field key -&gt; template string (e.g. {\&quot;email\&quot;:{\&quot;subject\&quot;:\&quot;...\&quot;,\&quot;body\&quot;:\&quot;...\&quot;}}) |  [optional] |
|**defaultChannels** | **List&lt;String&gt;** | Default channels (used when no subscription) |  [optional] |
|**name** | **String** | Human-readable name |  |
|**subscriptionId** | **String** | Subscription ID (null for standalone) |  [optional] |



