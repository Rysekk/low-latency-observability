resource "oci_objectstorage_bucket" "bucket-wg" {
  compartment_id = oci_identity_compartment.tf-compartment.id
  name           = "bucket-wireguard"
  namespace      = data.oci_objectstorage_namespace.storage-ns.namespace
  storage_tier   = "Standard"
}

resource "oci_identity_dynamic_group" "dynamic-group-wg" {
  #Required
  compartment_id = var.tenancy_ocid
  description    = "dynamique group for storage bucket of wireguards key"
  matching_rule  = "ANY {instance.compartment.id = '${oci_identity_compartment.tf-compartment.id}'}"
  name           = "dynamic-group-bucket-wg"
}


resource "oci_identity_policy" "dynamic-policy-wg" {
  name           = "dynamic-policy-wg"
  description    = "dynamic policy for storage bucket of wireguards key"
  compartment_id = var.tenancy_ocid

  statements = [
    "Allow dynamic-group ${oci_identity_dynamic_group.dynamic-group-wg.name} to manage objects in compartment id ${oci_identity_compartment.tf-compartment.id} where all {target.bucket.name = '${oci_objectstorage_bucket.bucket-wg.name}', target.object.name = 'wg/*', any {request.permission='OBJECT_CREATE', request.permission='OBJECT_READ', request.permission='OBJECT_INSPECT'}}",
  ]
}
