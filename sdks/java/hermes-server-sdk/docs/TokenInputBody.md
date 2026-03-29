

# TokenInputBody


## Properties

| Name | Type | Description | Notes |
|------------ | ------------- | ------------- | -------------|
|**$schema** | **URI** | A URL to the JSON Schema for this object. |  [optional] [readonly] |
|**expiresIn** | **Long** | Requested token lifetime in seconds (min 3600 &#x3D; 1h, max 604800 &#x3D; 7d, default 14400 &#x3D; 4h). The actual expiry includes ±10% random jitter to prevent thundering-herd token refreshes. |  [optional] |
|**tenantId** | **String** | Tenant identifier |  |
|**userId** | **String** | External user identifier |  |



