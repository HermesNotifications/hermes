# Hermes.ServerSdk.Model.ErrorModel

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Schema** | **string** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**Detail** | **string** | A human-readable explanation specific to this occurrence of the problem. | [optional] 
**Errors** | [**List&lt;ErrorDetail&gt;**](ErrorDetail.md) | Optional list of individual error details | [optional] 
**Instance** | **string** | A URI reference that identifies the specific occurrence of the problem. | [optional] 
**Status** | **long** | HTTP status code | [optional] 
**Title** | **string** | A short, human-readable summary of the problem type. This value should not change between occurrences of the error. | [optional] 
**Type** | **string** | A URI reference to human-readable documentation for the error. | [optional] [default to "about:blank"]

[[Back to Model list]](../../README.md#documentation-for-models) [[Back to API list]](../../README.md#documentation-for-api-endpoints) [[Back to README]](../../README.md)

