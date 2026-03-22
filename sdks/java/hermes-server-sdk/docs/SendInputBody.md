

# SendInputBody


## Properties

| Name | Type | Description | Notes |
|------------ | ------------- | ------------- | -------------|
|**$schema** | **URI** | A URL to the JSON Schema for this object. |  [optional] [readonly] |
|**channels** | **List&lt;String&gt;** | Explicit delivery channels |  [optional] |
|**content** | [**SendContent**](SendContent.md) | Direct content (mutually exclusive with type) |  [optional] |
|**data** | **Map&lt;String, Object&gt;** | Template data for rendering |  [optional] |
|**group** | **String** | Group slug (required for direct content sends) |  [optional] |
|**tenantId** | **String** | Tenant identifier |  |
|**type** | **String** | Notification type slug (mutually exclusive with content) |  [optional] |
|**userId** | **String** | External user identifier |  |



