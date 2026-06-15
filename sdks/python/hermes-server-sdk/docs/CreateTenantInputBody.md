# CreateTenantInputBody


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_schema** | **str** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**name** | **str** | Tenant name | 

## Example

```python
from hermes_server_sdk.models.create_tenant_input_body import CreateTenantInputBody

# TODO update the JSON string below
json = "{}"
# create an instance of CreateTenantInputBody from a JSON string
create_tenant_input_body_instance = CreateTenantInputBody.from_json(json)
# print the JSON string representation of the object
print(CreateTenantInputBody.to_json())

# convert the object into a dict
create_tenant_input_body_dict = create_tenant_input_body_instance.to_dict()
# create an instance of CreateTenantInputBody from a dict
create_tenant_input_body_from_dict = CreateTenantInputBody.from_dict(create_tenant_input_body_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


