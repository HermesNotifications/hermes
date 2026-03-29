# CreateAPIKeyInputBody


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_schema** | **str** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**name** | **str** | Human-readable key name | 
**permissions** | **List[str]** | Permission set (defaults to all except apikeys:manage) | [optional] 

## Example

```python
from hermes_server_sdk.models.create_api_key_input_body import CreateAPIKeyInputBody

# TODO update the JSON string below
json = "{}"
# create an instance of CreateAPIKeyInputBody from a JSON string
create_api_key_input_body_instance = CreateAPIKeyInputBody.from_json(json)
# print the JSON string representation of the object
print(CreateAPIKeyInputBody.to_json())

# convert the object into a dict
create_api_key_input_body_dict = create_api_key_input_body_instance.to_dict()
# create an instance of CreateAPIKeyInputBody from a dict
create_api_key_input_body_from_dict = CreateAPIKeyInputBody.from_dict(create_api_key_input_body_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


