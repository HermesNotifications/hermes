# SendInputBodyMetadata

Opaque metadata echoed back on the notification. Hermes reads only 'level' and 'toast'.

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**level** | **str** | How a client should present this notification. | [optional] 
**toast** | **bool** | Whether a client should surface this transiently rather than waiting for the user to open their inbox. | [optional] 

## Example

```python
from hermes_server_sdk.models.send_input_body_metadata import SendInputBodyMetadata

# TODO update the JSON string below
json = "{}"
# create an instance of SendInputBodyMetadata from a JSON string
send_input_body_metadata_instance = SendInputBodyMetadata.from_json(json)
# print the JSON string representation of the object
print(SendInputBodyMetadata.to_json())

# convert the object into a dict
send_input_body_metadata_dict = send_input_body_metadata_instance.to_dict()
# create an instance of SendInputBodyMetadata from a dict
send_input_body_metadata_from_dict = SendInputBodyMetadata.from_dict(send_input_body_metadata_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


