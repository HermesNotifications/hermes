

# UpdateCategoryInputBody


## Properties

| Name | Type | Description | Notes |
|------------ | ------------- | ------------- | -------------|
|**$schema** | **URI** | A URL to the JSON Schema for this object. |  [optional] [readonly] |
|**defaultChannels** | **List&lt;String&gt;** | Default delivery channels |  |
|**defaultState** | [**DefaultStateEnum**](#DefaultStateEnum) | Default subscription state |  |
|**name** | **String** | Human-readable name |  |
|**sortOrder** | **Long** | Display order |  |



## Enum: DefaultStateEnum

| Name | Value |
|---- | -----|
| ON | &quot;on&quot; |
| OFF | &quot;off&quot; |
| REQUIRED | &quot;required&quot; |



