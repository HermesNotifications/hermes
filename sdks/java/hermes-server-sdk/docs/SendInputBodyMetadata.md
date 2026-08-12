

# SendInputBodyMetadata

Opaque metadata echoed back on the notification. Hermes reads only 'level' and 'toast'.

## Properties

| Name | Type | Description | Notes |
|------------ | ------------- | ------------- | -------------|
|**level** | [**LevelEnum**](#LevelEnum) | How a client should present this notification. |  [optional] |
|**toast** | **Boolean** | Whether a client should surface this transiently rather than waiting for the user to open their inbox. |  [optional] |



## Enum: LevelEnum

| Name | Value |
|---- | -----|
| INFO | &quot;info&quot; |
| SUCCESS | &quot;success&quot; |
| WARNING | &quot;warning&quot; |
| ERROR | &quot;error&quot; |



