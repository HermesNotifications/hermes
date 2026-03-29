

# SendInputBody


## Properties

| Name | Type | Description | Notes |
|------------ | ------------- | ------------- | -------------|
|**$schema** | **URI** | A URL to the JSON Schema for this object. |  [optional] [readonly] |
|**channels** | **List&lt;String&gt;** | Explicit delivery channels |  [optional] |
|**content** | [**SendContent**](SendContent.md) | Direct content (mutually exclusive with template) |  [optional] |
|**data** | **Map&lt;String, Object&gt;** | Template data for rendering |  [optional] |
|**template** | **String** | Notification template slug (mutually exclusive with content) |  [optional] |
|**to** | [**SendRecipient**](SendRecipient.md) | Notification recipient |  |



