# TokenInputBody


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_schema** | **str** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**expires_in** | **int** | Requested token lifetime in seconds (min 3600 &#x3D; 1h, max 604800 &#x3D; 7d, default 14400 &#x3D; 4h). The actual expiry includes ±10% random jitter to prevent thundering-herd token refreshes. | [optional] 
**organization_id** | **str** | Organization identifier | 
**user_id** | **str** | External user identifier | 

## Example

```python
from hermes_server_sdk.models.token_input_body import TokenInputBody

# TODO update the JSON string below
json = "{}"
# create an instance of TokenInputBody from a JSON string
token_input_body_instance = TokenInputBody.from_json(json)
# print the JSON string representation of the object
print(TokenInputBody.to_json())

# convert the object into a dict
token_input_body_dict = token_input_body_instance.to_dict()
# create an instance of TokenInputBody from a dict
token_input_body_from_dict = TokenInputBody.from_dict(token_input_body_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


