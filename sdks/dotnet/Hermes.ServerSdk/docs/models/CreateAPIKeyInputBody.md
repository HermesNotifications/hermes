# Hermes.ServerSdk.Model.CreateAPIKeyInputBody

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Name** | **string** | Human-readable key name | 
**Schema** | **string** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**Permissions** | **List&lt;string&gt;** | Permission set (defaults to all except apikeys:manage) | [optional] 
**RateLimitBurst** | **long** | Requests admitted instantaneously for this key. Omit to use the service default. | [optional] 
**RateLimitPerSecond** | **long** | Sustained requests per second for this key. Omit to use the service default. | [optional] 

[[Back to Model list]](../../README.md#documentation-for-models) [[Back to API list]](../../README.md#documentation-for-api-endpoints) [[Back to README]](../../README.md)

