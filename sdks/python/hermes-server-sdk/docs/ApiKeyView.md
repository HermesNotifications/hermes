# ApiKeyView


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_schema** | **str** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**created_at** | **datetime** |  | 
**id** | **str** |  | 
**name** | **str** |  | 
**permissions** | **List[str]** |  | 
**rate_limit_burst** | **int** | Absent means the service default applies. | [optional] 
**rate_limit_per_second** | **int** | Absent means the service default applies. | [optional] 

## Example

```python
from hermes_server_sdk.models.api_key_view import ApiKeyView

# TODO update the JSON string below
json = "{}"
# create an instance of ApiKeyView from a JSON string
api_key_view_instance = ApiKeyView.from_json(json)
# print the JSON string representation of the object
print(ApiKeyView.to_json())

# convert the object into a dict
api_key_view_dict = api_key_view_instance.to_dict()
# create an instance of ApiKeyView from a dict
api_key_view_from_dict = ApiKeyView.from_dict(api_key_view_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


