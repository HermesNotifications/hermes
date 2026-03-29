# SendRecipient


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**email** | **str** | Optional email address for this notification | [optional] 
**phone** | **str** | Optional phone number for this notification | [optional] 
**tenant_id** | **str** | Tenant identifier | 
**user_id** | **str** | External user identifier | 

## Example

```python
from hermes_server_sdk.models.send_recipient import SendRecipient

# TODO update the JSON string below
json = "{}"
# create an instance of SendRecipient from a JSON string
send_recipient_instance = SendRecipient.from_json(json)
# print the JSON string representation of the object
print(SendRecipient.to_json())

# convert the object into a dict
send_recipient_dict = send_recipient_instance.to_dict()
# create an instance of SendRecipient from a dict
send_recipient_from_dict = SendRecipient.from_dict(send_recipient_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


