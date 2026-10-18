resource "oci_core_instance" "ubuntu_instance" {
    # Required
    availability_domain = data.oci_identity_availability_domains.ads.availability_domains[0].name
    compartment_id = oci_identity_compartment.tf-compartment.id
    shape = "VM.Standard.A1.Flex"
    shape_config {
        ocpus = "2"
        memory_in_gbs = "12"
    }
    source_details {
        source_id = data.oci_core_images.ubuntu_image.images[0].id
        source_type = "image"
    }

    # Optional
    display_name = "k3s-prod"
    create_vnic_details {
        assign_public_ip = true
        subnet_id = oci_core_subnet.subnet.id
    }
    metadata = {
        ssh_authorized_keys = file("${var.ssh_public_key_path}")
          user_data = base64encode(templatefile("${path.module}/cloud-init.yaml", {
            wireguard_public_key = var.wg_laptop_public_key
            os_namespace         = data.oci_objectstorage_namespace.storage-ns.namespace
            wg_bucket            = oci_objectstorage_bucket.bucket-wg.name
  }))
    } 
    preserve_boot_volume = false
    depends_on = [oci_identity_policy.dynamic-policy-wg]

    # Don't replace the instance when a newer Ubuntu image is published
    lifecycle {
        ignore_changes = [source_details[0].source_id]
    }
}

output "instance_ip_public" {
  value     = oci_core_instance.ubuntu_instance.public_ip
}