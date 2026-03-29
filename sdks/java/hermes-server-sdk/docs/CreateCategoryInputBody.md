

# CreateCategoryInputBody


## Properties

| Name | Type | Description | Notes |
|------------ | ------------- | ------------- | -------------|
|**$schema** | **URI** | A URL to the JSON Schema for this object. |  [optional] [readonly] |
|**defaultChannels** | **List&lt;String&gt;** | Default delivery channels |  [optional] |
|**defaultState** | [**DefaultStateEnum**](#DefaultStateEnum) | Default subscription state |  |
|**name** | **String** | Human-readable name |  |
|**slug** | **String** | URL-friendly identifier |  |
|**sortOrder** | **Long** | Display order |  [optional] |



## Enum: DefaultStateEnum

| Name | Value |
|---- | -----|
| ON | &quot;on&quot; |
| OFF | &quot;off&quot; |
| REQUIRED | &quot;required&quot; |



