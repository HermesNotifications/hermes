# TokenOutputBody


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_schema** | **str** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**expires_at** | **str** | Token expiration time in RFC3339 format | 
**token** | **str** | JWT token for user-facing API access | 

## Example

```python
from hermes_server_sdk.models.token_output_body import TokenOutputBody

# TODO update the JSON string below
json = "{}"
# create an instance of TokenOutputBody from a JSON string
token_output_body_instance = TokenOutputBody.from_json(json)
# print the JSON string representation of the object
print(TokenOutputBody.to_json())

# convert the object into a dict
token_output_body_dict = token_output_body_instance.to_dict()
# create an instance of TokenOutputBody from a dict
token_output_body_from_dict = TokenOutputBody.from_dict(token_output_body_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


