# SetAPIKeyRateLimitInputBody


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_schema** | **str** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**burst** | **int** | Requests admitted instantaneously. Omit to reset to the service default. | [optional] 
**per_second** | **int** | Sustained requests per second. Omit to reset to the service default. | [optional] 

## Example

```python
from hermes_server_sdk.models.set_api_key_rate_limit_input_body import SetAPIKeyRateLimitInputBody

# TODO update the JSON string below
json = "{}"
# create an instance of SetAPIKeyRateLimitInputBody from a JSON string
set_api_key_rate_limit_input_body_instance = SetAPIKeyRateLimitInputBody.from_json(json)
# print the JSON string representation of the object
print(SetAPIKeyRateLimitInputBody.to_json())

# convert the object into a dict
set_api_key_rate_limit_input_body_dict = set_api_key_rate_limit_input_body_instance.to_dict()
# create an instance of SetAPIKeyRateLimitInputBody from a dict
set_api_key_rate_limit_input_body_from_dict = SetAPIKeyRateLimitInputBody.from_dict(set_api_key_rate_limit_input_body_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


