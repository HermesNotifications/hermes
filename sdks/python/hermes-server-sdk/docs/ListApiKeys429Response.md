# ListApiKeys429Response


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**error** | **str** | Human-readable reason. | [optional] 

## Example

```python
from hermes_server_sdk.models.list_api_keys429_response import ListApiKeys429Response

# TODO update the JSON string below
json = "{}"
# create an instance of ListApiKeys429Response from a JSON string
list_api_keys429_response_instance = ListApiKeys429Response.from_json(json)
# print the JSON string representation of the object
print(ListApiKeys429Response.to_json())

# convert the object into a dict
list_api_keys429_response_dict = list_api_keys429_response_instance.to_dict()
# create an instance of ListApiKeys429Response from a dict
list_api_keys429_response_from_dict = ListApiKeys429Response.from_dict(list_api_keys429_response_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


