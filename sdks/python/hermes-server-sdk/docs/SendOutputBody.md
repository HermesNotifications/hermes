# SendOutputBody


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_schema** | **str** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**notification_id** | **str** | ID of the created notification | 

## Example

```python
from hermes_server_sdk.models.send_output_body import SendOutputBody

# TODO update the JSON string below
json = "{}"
# create an instance of SendOutputBody from a JSON string
send_output_body_instance = SendOutputBody.from_json(json)
# print the JSON string representation of the object
print(SendOutputBody.to_json())

# convert the object into a dict
send_output_body_dict = send_output_body_instance.to_dict()
# create an instance of SendOutputBody from a dict
send_output_body_from_dict = SendOutputBody.from_dict(send_output_body_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


