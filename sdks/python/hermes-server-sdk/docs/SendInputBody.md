# SendInputBody


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_schema** | **str** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**channels** | **List[str]** | Explicit delivery channels | [optional] 
**content** | [**SendContent**](SendContent.md) | Direct content (mutually exclusive with template) | [optional] 
**data** | **Dict[str, object]** | Template data for rendering | [optional] 
**metadata** | [**SendInputBodyMetadata**](SendInputBodyMetadata.md) |  | [optional] 
**template** | **str** | Notification template slug (mutually exclusive with content) | [optional] 
**to** | [**SendRecipient**](SendRecipient.md) | Notification recipient | 

## Example

```python
from hermes_server_sdk.models.send_input_body import SendInputBody

# TODO update the JSON string below
json = "{}"
# create an instance of SendInputBody from a JSON string
send_input_body_instance = SendInputBody.from_json(json)
# print the JSON string representation of the object
print(SendInputBody.to_json())

# convert the object into a dict
send_input_body_dict = send_input_body_instance.to_dict()
# create an instance of SendInputBody from a dict
send_input_body_from_dict = SendInputBody.from_dict(send_input_body_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


