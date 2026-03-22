

# ErrorModel


## Properties

| Name | Type | Description | Notes |
|------------ | ------------- | ------------- | -------------|
|**$schema** | **URI** | A URL to the JSON Schema for this object. |  [optional] [readonly] |
|**detail** | **String** | A human-readable explanation specific to this occurrence of the problem. |  [optional] |
|**errors** | [**List&lt;ErrorDetail&gt;**](ErrorDetail.md) | Optional list of individual error details |  [optional] |
|**instance** | **URI** | A URI reference that identifies the specific occurrence of the problem. |  [optional] |
|**status** | **Long** | HTTP status code |  [optional] |
|**title** | **String** | A short, human-readable summary of the problem type. This value should not change between occurrences of the error. |  [optional] |
|**type** | **URI** | A URI reference to human-readable documentation for the error. |  [optional] |



