# Hermes.ServerSdk.Model.UpdateTemplateInputBody

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Name** | **string** | Human-readable name | 
**Schema** | **string** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**Content** | **Dictionary&lt;string, Dictionary&lt;string, string&gt;&gt;** | Per-channel content: channel slug -&gt; field key -&gt; template string (e.g. {\&quot;email\&quot;:{\&quot;subject\&quot;:\&quot;...\&quot;,\&quot;body\&quot;:\&quot;...\&quot;}}) | [optional] 
**DefaultChannels** | **List&lt;string&gt;** | Default channels (used when no subscription) | [optional] 
**SubscriptionId** | **string** | Subscription ID (null for standalone) | [optional] 

[[Back to Model list]](../../README.md#documentation-for-models) [[Back to API list]](../../README.md#documentation-for-api-endpoints) [[Back to README]](../../README.md)

