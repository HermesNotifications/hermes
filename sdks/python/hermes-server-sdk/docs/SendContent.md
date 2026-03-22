# SendContent


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**action_label** | **str** | Optional action button label | [optional] 
**action_url** | **str** | Optional action URL | [optional] 
**body** | **str** | Notification body | 
**title** | **str** | Notification title | 

## Example

```python
from hermes_server_sdk.models.send_content import SendContent

# TODO update the JSON string below
json = "{}"
# create an instance of SendContent from a JSON string
send_content_instance = SendContent.from_json(json)
# print the JSON string representation of the object
print(SendContent.to_json())

# convert the object into a dict
send_content_dict = send_content_instance.to_dict()
# create an instance of SendContent from a dict
send_content_from_dict = SendContent.from_dict(send_content_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


