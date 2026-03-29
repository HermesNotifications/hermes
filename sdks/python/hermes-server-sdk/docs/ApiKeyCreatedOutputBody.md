# ApiKeyCreatedOutputBody


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_schema** | **str** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**created_at** | **datetime** |  | 
**id** | **str** |  | 
**name** | **str** |  | 
**permissions** | **List[str]** |  | 
**raw_key** | **str** |  | 

## Example

```python
from hermes_server_sdk.models.api_key_created_output_body import ApiKeyCreatedOutputBody

# TODO update the JSON string below
json = "{}"
# create an instance of ApiKeyCreatedOutputBody from a JSON string
api_key_created_output_body_instance = ApiKeyCreatedOutputBody.from_json(json)
# print the JSON string representation of the object
print(ApiKeyCreatedOutputBody.to_json())

# convert the object into a dict
api_key_created_output_body_dict = api_key_created_output_body_instance.to_dict()
# create an instance of ApiKeyCreatedOutputBody from a dict
api_key_created_output_body_from_dict = ApiKeyCreatedOutputBody.from_dict(api_key_created_output_body_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


